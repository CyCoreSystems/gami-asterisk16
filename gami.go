package gami

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"sync"
)

const (
	_LINE_TERM    = "\r\n"            // packet line separator
	_KEY_VAL_TERM = ":"               // header value separator
	_READ_BUF     = 512               // buffer size for socket reader
	_CMD_END      = "--END COMMAND--" // Asterisk command data end
	_HOST         = "gami"            // default host value
	ORIG_TMOUT    = 30000             // Originate timeout
	VER           = 0.2
)

var (
	_PT_BYTES = []byte(_LINE_TERM + _LINE_TERM) // packet separator
)

// basic Asterisk message
type Message map[string]string

// action id generator
type Aid struct {
	host string
	id   int
	mu   *sync.RWMutex
}

// NewAid, Aid factory
func NewAid() *Aid {

	var err error
	a := &Aid{}
	a.mu = &sync.RWMutex{}

	if a.host, err = os.Hostname(); err != nil {
		a.host = _HOST
	}

	return a
}

// GenerateId, generate new action id
func (a *Aid) Generate() string {

	a.mu.Lock()
	defer a.mu.Unlock()
	a.id++
	return a.host + "-" + fmt.Sprint(a.id)
}

// callback function storage
type cbList struct {
	mu *sync.RWMutex
	f  map[string]*func(Message)
	sd map[string]bool // callback will self delete (used for multi-message responses)
}

// set, setting handle function for specific action id|event (will overwrite current if present)
func (cbl *cbList) set(key string, f *func(Message), sd bool) {

	cbl.mu.Lock()
	defer cbl.mu.Unlock()
	cbl.f[key] = f
	cbl.sd[key] = sd
}

// del, deleting callback for specific action id|event
func (cbl *cbList) del(key string) {

	cbl.mu.Lock()
	defer cbl.mu.Unlock()
	delete(cbl.f, key)
	delete(cbl.sd, key)
}

// get, returns function for specific action id/event
func (cbl *cbList) get(key string) (*func(Message), bool) {

	cbl.mu.RLock()
	defer cbl.mu.RUnlock()
	return cbl.f[key], cbl.sd[key]
}

// Originate, struct used in Originate command
// if pointed Context and Application, Context has higher priority
type Originate struct {
	Channel  string // channel to which originate
	Context  string // context to move after originate success
	Exten    string // exten to move after originate success
	Priority string // priority to move after originate success
	Timeout  int    // originate timeout ms
	CallerID string // caller identification string
	Account  string // used for CDR

	Application string // application to execute after successful originate
	Data        string // data passed to application

	Async bool // asynchronous call
}

// NewOriginate, Originate default values constructor (to context)
func NewOriginate(channel, context, exten, priority string) *Originate {
	return &Originate{
		Channel:  channel,
		Context:  context,
		Exten:    exten,
		Priority: priority,
		Timeout:  ORIG_TMOUT,
		Async:    false,
	}
}

// NewOriginateApp, constructor for originate to application
func NewOriginateApp(channel, app, data string) *Originate {
	return &Originate{
		Channel:     channel,
		Timeout:     ORIG_TMOUT,
		Application: app,
		Data:        data,
		Async:       false,
	}
}

// main working entity
type Asterisk struct {
	conn           *net.Conn      // network connection to Asterisk
	actionHandlers *cbList        // action response handle functions
	eventHandlers  *cbList        // event handle functions
	defaultHandler *func(Message) // default handler for all Asterisk messages, useful for debugging
	netErrHandler  *func(error)   // network error handle function
	aid            *Aid           // action id
	authorized     bool           // is successful logined to AMI
}

// NewAsterisk, Asterisk factory
func NewAsterisk(conn *net.Conn, f *func(error)) *Asterisk {

	return &Asterisk{
		conn: conn,
		actionHandlers: &cbList{
			&sync.RWMutex{},
			make(map[string]*func(Message)),
			make(map[string]bool),
		},
		eventHandlers: &cbList{
			&sync.RWMutex{},
			make(map[string]*func(Message)),
			make(map[string]bool),
		},
		aid:           NewAid(),
		netErrHandler: f,
	}
}

// DefaultHandler, set default handler for all Asterisk messages
func (a *Asterisk) DefaultHandler(f *func(Message)) {

	a.defaultHandler = f
}

// Login, logins to AMI and starts read dispatcher
func (a *Asterisk) Login(login string, password string) error {

	go a.readDispatcher()

	lhc := make(chan error)
	lhf := func(m Message) {
		if m["Response"] == "Success" {
			lhc <- nil
		} else {
			lhc <- fmt.Errorf("%s", m["Message"])
		}
	}

	aid := a.aid.Generate()
	a.actionHandlers.set(aid, &lhf, false)

	m := Message{
		"Action":   "Login",
		"Username": login,
		"Secret":   password,
		"ActionID": aid,
	}
	a.send(m)

	err := <-lhc

	if err != nil {
		return err
	}

	a.authorized = true

	return nil
}

// SendAction, universal action send
func (a *Asterisk) SendAction(m Message, f *func(m Message)) error {

	if !a.authorized {
		return fmt.Errorf("Not authorized")
	}

	m["ActionID"] = a.aid.Generate()

	if f != nil {
		a.actionHandlers.set(m["ActionID"], f, false)
	}

	return a.send(m)
}

// HoldCallbackAction, send action with callback which deletes itself (used for multi-line responses)
// IMPORTANT: callback function must delete itself by own
func (a *Asterisk) HoldCallbackAction(m Message, f *func(m Message)) error {

	if !a.authorized {
		return fmt.Errorf("Not authorized")
	}

	m["ActionID"] = a.aid.Generate()

	if f == nil {
		return fmt.Errorf("Use SendAction with nil callback!")
	}

	a.actionHandlers.set(m["ActionID"], f, true)

	return a.send(m)
}

// DelCallback, delete action callback (used by self-delete callbacks)
func (a *Asterisk) DelCallback(m Message) {

	a.actionHandlers.del(m["ActionID"])
}

// Hangup, hangup Asterisk channel
func (a Asterisk) Hangup(channel string, f *func(Message)) error {

	m := Message{
		"Action":  "Hangup",
		"Channel": channel,
	}

	return a.SendAction(m, f)
}

// Redirect, redirect Asterisk channel
func (a Asterisk) Redirect(channel string, context string, exten string, priority string, f *func(Message)) error {

	m := Message{
		"Action":   "Redirect",
		"Channel":  channel,
		"Context":  context,
		"Exten":    exten,
		"Priority": priority,
	}

	return a.SendAction(m, f)
}

// Logoff, logoff from AMI
func (a Asterisk) Logoff() error {

	m := Message{
		"Action": "Logoff",
	}

	return a.SendAction(m, nil)
}

// Originate, make a call
func (a *Asterisk) Originate(o *Originate, vars map[string]string, f *func(Message)) error {

	m := Message{
		"Action":   "Originate",
		"Channel":  o.Channel,
		"Account":  o.Account,
		"Timeout":  fmt.Sprint(o.Timeout),
		"CallerID": o.CallerID,
	}

	if o.Async {
		m["Async"] = "yes"
	}

	if o.Context == "" {
		m["Application"] = o.Application
		m["Data"] = o.Data
	} else {
		m["Context"] = o.Context
		m["Exten"] = o.Exten
		m["Priority"] = o.Priority
	}

	if vars != nil {
		var vl string

		for k, v := range vars {
			vl += k + "=" + v + ","
		}

		m["Variable"] = vl[:len(vl)-1]
	}

	return a.SendAction(m, f)
}

// RegisterHandler, register callback for Asterisk event (one handler per event)
// return err if handler already exists
func (a *Asterisk) RegisterHandler(event string, f *func(m Message)) error {

	if f, _ := a.eventHandlers.get(event); f != nil {
		return fmt.Errorf("Handler already exist for event %s", event)
	}

	a.eventHandlers.set(event, f, false)

	return nil
}

// UnregisterHandler, deregister callback for event
func (a *Asterisk) UnregisterHandler(event string) {
	a.eventHandlers.del(event)
}

// Bridge, bridge two channels already in the PBX
func (a *Asterisk) Bridge(chan1, chan2 string, tone bool, f *func(Message)) error {

	t := "no"
	if tone {
		t = "yes"
	}

	m := Message{
		"Action":   "Bridge",
		"Channel1": chan1,
		"Channel2": chan2,
		"Tone":     t,
	}

	return a.SendAction(m, f)
}

// Command, execute Asterisk CLI Command
func (a *Asterisk) Command(cmd string, f *func(Message)) error {
	m := Message{
		"Action":  "Command",
		"Command": cmd,
	}

	return a.SendAction(m, f)
}

// ConfbridgeList, list participants in a conference (generates multimessage response)
func (a *Asterisk) ConfbridgeList(conference string, f *func(Message)) error {
	m := Message{
		"Action":     "ConfbridgeList",
		"Conference": conference,
	}

	return a.SendAction(m, f)
}

// GetConfbridgeList, returns conference participants, blocks untill end
func (a *Asterisk) GetConfbridgeList(conference string) ([]Message, error) {
	m := Message{
		"Action":     "ConfbridgeList",
		"Conference": conference,
	}

	ml := []Message{}
	mc := make(chan []Message)

	clj := func(m Message) {
		if m["Event"] == "ConfbridgeListComplete" && m["EventList"] == "Complete" ||
			m["Response"] == "Error" {
			a.DelCallback(m)
			mc <- ml
		}
		if m["EventList"] == "start" {
			return
		}
		ml = append(ml, m)
	}

	err := a.HoldCallbackAction(m, &clj)

	if err != nil {
		return nil, err
	}

	return <-mc, nil
}

// ConfbridgeKick, kick a Confbridge user
func (a *Asterisk) ConfbridgeKick(conf, chann string, f *func(Message)) error {
	m := Message{
		"Action":     "ConfbridgeKick",
		"Conference": conf,
		"Channel":    chann,
	}

	return a.SendAction(m, f)
}

// ConfbridgeToggleMute, mute/unmute a Confbridge user
func (a *Asterisk) ConfbridgeToggleMute(conf, chann string, mute bool, f *func(Message)) error {
	m := Message{
		"Conference": conf,
		"Channel":    chann,
	}

	if mute {
		m["Action"] = "ConfbridgeMute"
	} else {
		m["Action"] = "ConfbridgeUnmute"
	}

	return a.SendAction(m, f)
}

// ConfbridgeStartRecord, start conference record
func (a *Asterisk) ConfbridgeStartRecord(conf, file string, f *func(Message)) error {

	m := Message{
		"Action":     "ConfbridgeStartRecord",
		"Conference": conf,
	}

	if file != "" {
		m["RecordFile"] = file
	}

	return a.SendAction(m, f)
}

// ConfbridgeStopRecord, stop conference record
func (a *Asterisk) ConfbridgeStopRecord(conf string, f *func(Message)) error {
	m := Message{
		"Action":     "ConfbridgeStopRecord",
		"Conference": conf,
	}

	return a.SendAction(m, f)
}

// MeetmeList, list participants in a MeetMe conference (generates multimessage response)
// if conference empty string will return for all conferences
func (a *Asterisk) MeetmeList(conference string, f *func(Message)) error {
	m := Message{
		"Action": "MeetmeList",
	}

	if conference != "" {
		m["Conference"] = conference
	}

	return a.SendAction(m, f)
}

// GetMeetmeList, returns MeetMe conference participants, blocks untill end
func (a *Asterisk) GetMeetmeList(conference string) ([]Message, error) {
	m := Message{
		"Action": "MeetmeList",
	}

	if conference != "" {
		m["Conference"] = conference
	}

	ml := []Message{}
	mc := make(chan []Message)

	clj := func(m Message) {
		if m["Event"] == "MeetmeListComplete" && m["EventList"] == "Complete" ||
			m["Response"] == "Error" {
			a.DelCallback(m)
			mc <- ml
		}
		if m["EventList"] == "start" {
			return
		}
		ml = append(ml, m)
	}

	err := a.HoldCallbackAction(m, &clj)

	if err != nil {
		return nil, err
	}

	return <-mc, nil
}

// ModuleLoad, loads, unloads or reloads an Asterisk module in a running system
func (a *Asterisk) ModuleLoad(module, tload string, f *func(Message)) error {

	m := Message{
		"Action":   "ModuleLoad",
		"LoadType": tload,
		"Module":   module,
	}

	return a.SendAction(m, f)
}

// Reload, reload Asterisk module
func (a *Asterisk) Reaload(module string, f *func(Message)) error {

	m := Message{
		"Action": "Reload",
		"Module": module,
	}

	return a.SendAction(m, f)
}

// UserEvent, send an arbitrary event
func (a *Asterisk) UserEvent(name string, headers map[string]string, f *func(Message)) error {
	m := Message{
		"Action":    "UserEvent",
		"UserEvent": name,
	}

	for k, v := range headers {
		m[k] = v
	}

	return a.SendAction(m, f)
}

// send, send Message to socket
func (a *Asterisk) send(m Message) error {

	buf := bytes.NewBufferString("")

	for k, v := range m {
		buf.Write([]byte(k))
		buf.Write([]byte(_KEY_VAL_TERM))
		buf.Write([]byte(v))
		buf.Write([]byte(_LINE_TERM))
	}
	buf.Write([]byte(_LINE_TERM))

	if wrb, err := (*a.conn).Write(buf.Bytes()); wrb != buf.Len() || err != nil {
		if err != nil {
			return err
		}
		return fmt.Errorf("Not fully writed packet to output stream\n")
	}

	return nil
}

// readDispatcher, reads data from socket and builds messages
func (a *Asterisk) readDispatcher() {

	r := bufio.NewReader(*a.conn)
	pbuf := bytes.NewBufferString("") // data buffer
	buf := make([]byte, _READ_BUF)    // read buffer

	for {
		rc, err := r.Read(buf)

		if err != nil { // network error
			a.authorized = false // unauth

			if a.netErrHandler != nil { // run network error callback
				(*a.netErrHandler)(err)
			}
			return
		}

		wb, err := pbuf.Write(buf[:rc])

		if err != nil || wb != rc { // can't write to data buffer, just skip
			continue
		}

		// while has end of packet symbols in buffer
		for pos := bytes.Index(pbuf.Bytes(), _PT_BYTES); pos != -1; pos = bytes.Index(pbuf.Bytes(), _PT_BYTES) {

			bp := make([]byte, pos+len(_PT_BYTES))
			r, err := pbuf.Read(bp)                    // reading packet to separate puffer
			if err != nil || r != pos+len(_PT_BYTES) { // reading problems, just skip
				continue
			}

			m := make(Message)

			// splitting packet by line separator
			for _, line := range bytes.Split(bp, []byte(_LINE_TERM)) {

				// empty line
				if len(line) == 0 {
					continue
				}

				kvl := bytes.Split(line, []byte(_KEY_VAL_TERM))

				// not standard header
				if len(kvl) == 1 {
					if string(line) != _CMD_END {
						m["CmdData"] += string(line)
					}
					continue
				}

				k := bytes.TrimSpace(kvl[0])
				v := bytes.TrimSpace(kvl[1])
				m[string(k)] = string(v)
			}

			// if has ActionID and has callback run it and delete
			if v, vok := m["ActionID"]; vok {
				if f, sd := a.actionHandlers.get(v); f != nil {
					go (*f)(m)
					if !sd { // will never remove "self-delete" callbacks
						a.actionHandlers.del(v)
					}
				}
			}

			// if Event and has callback run it
			if v, vok := m["Event"]; vok {
				if f, _ := a.eventHandlers.get(v); f != nil {
					go (*f)(m)
				}
			}

			// run default handler if not nil
			if a.defaultHandler != nil {
				go (*a.defaultHandler)(m)
			}
		}
	}
}
