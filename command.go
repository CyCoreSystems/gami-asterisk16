package gami

import (
	"fmt"
)

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
