/* 
 Package gami implements simple Asterisk Manager Interface library.

 It's not handle any network layer, just parsing or creating packets
 and runs callback for it (if registered).

 Start working:

  conn, err := net.Dial("tcp", "astserver:5038")
  if err != nil {
      fmt.Fprintf(os.Stderr, "Can't connect " + err.Error() + "\n")
      return
  }

  var a gami.Asterisk
  a = gami.Asterisk{C:conn, Debug:true, Writer:os.Stdout} // will print all packets to stdout
  err = a.Handle() // preparing and starting reader and parser goroutines
  if err != nil {
      <error handling>
      return
  }

 Login to Asterisk (package not auto logins!):

  loginErr := a.Login("username", "password")
  if loginErr != nil { <error handling> }

 Placing a call:

  a.OriginateApp("SIP/myphone", "Playback", "hello-world, 
    "", "User <myphone>", "", true, nil, nil) // call to SIP/myphone and play hello-world to it

 Placing simple (any other too) command:

  ping := map[string]string { "Action":"Ping", }
  a.SendAction(ping, nil) // SendAction sends raw Asterisk command to AMI,
                          // it can be used for login with custom handle function

 Event handlers:

  hangupHandler := func(m gami.Message) {
    fmt.Printf("Hangup event received for channel %s\n", m["Channel"])
  }

  a.RegisterHandler("Hangup", hangupHandler)
  ...
  a.UnregisterHandler("Hangup")

 Finishing working:

  a.Logoff(nil)
*/
package gami

// Author: Vasiliy Kovalenko <dev.boot@gmail.com>
//
// TODO:
//  - implement multipacket response parsing (CoreShowChannels, SIPpeers)

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
)

const (
	T       = "\r\n"            // packet terminator
	C_T     = "--END COMMAND--" // command terminator
	HOST    = "gami_host"       // if os.Hostname not successful will use this for actionid
	O_TMOUT = "30000"           // originate timeout default value in ms
	VER     = 0.1               // package version
)

// asterisk Message, response for command or event
type Message map[string]string

// main struct for interacting to module
type Asterisk struct {
	C             net.Conn                 // network connection
	Debug         bool                     // will print all message to Writer
	Writer        io.Writer                // all messages from asterisk will come there if Debug is true
	ch            chan string              // channel for interacting between read and parse goroutine
	callbacks     map[string]func(Message) // map for callback functions
	cMutex        *sync.RWMutex            // mutex for callbacks
	eventHandlers map[string]func(Message) // map for event callbacks
	eMutex        *sync.RWMutex            // mutex for events
	necb          *func(error)             // network error callback
	hostname      string                   // hostname stored for generate ActionID
	id            int                      // integer indentificator for action
}

// parseMessage, asterisk Message parser from string array
func parseMessage(s []string) (m Message) {

	m = make(Message)

	for _, l := range s {

		res := strings.Split(l, ":")
		if len(res) == 1 {
			if l != C_T+T {
				m["CmdData"] = strings.Join([]string{m["CmdData"], l}, "")
			}
			continue
		}

		f := strings.TrimSpace(res[0])
		v := strings.Trim(res[1], "\r\n")
		v = strings.TrimSpace(v)
		m[f] = v
	}

	return
}

// updateActionCallback, add, remove or update callback for action
// if c == nil will remove callback, otherways replace or add
func (a *Asterisk) updateActionCallback(key string, c func(Message)) {

	a.cMutex.Lock()
	defer a.cMutex.Unlock()

	if c == nil {
		delete(a.callbacks, key)
		return
	}

	a.callbacks[key] = c
}

// updateEventCallback, add, remove or update callback for event
func (a *Asterisk) updateEventCallback(key string, c func(Message)) {

	a.eMutex.Lock()
	defer a.eMutex.Unlock()

	if c == nil {
		delete(a.eventHandlers, key)
		return
	}

	a.eventHandlers[key] = c
}

// Handle, preparing gami for working with Asterisk
func (a *Asterisk) Handle() (err error) {

	a.ch = make(chan string, 20)
	a.callbacks = make(map[string]func(Message))
	a.eventHandlers = make(map[string]func(Message))
	a.cMutex = &sync.RWMutex{}
	a.eMutex = &sync.RWMutex{}

	quit := make(chan int)

	if a.hostname, err = os.Hostname(); err != nil {
		a.hostname = HOST
	}

	// skipping version string
	r := bufio.NewReader(a.C)
	_, err = r.ReadString('\n')
	if err != nil {
		return
	}

	// reads data from connection and send it to 
	// handler channel (exit on error)
	go func() {
		for {
			data, err := r.ReadString('\n')
			a.ch <- data

			if err != nil {
				quit <- 1

				cb := *a.necb
				if cb != nil {
					defer cb(err)
				}

				return
			}
		}
	}()

	// reads data from channel, collecting packet
	// after parsing it and execute callback if exists
	go func() {

		p := []string{}

		for {
			select {
			case d := <-a.ch:

				if d == T { // received packet terminator
					var m Message       // initiate message struct, simple map wrapper
					m = parseMessage(p) // parsing message
					p = []string{}      // initiate new packet buffer

					if f, ok := a.callbacks[m["ActionID"]]; ok {
						go f(m)
						a.updateActionCallback(m["ActionID"], nil)
					}

					if v, vok := m["Event"]; vok {
						if f, fok := a.eventHandlers[v]; fok {
							go f(m)
						}
					}

					if a.Debug {
						fmt.Fprintf(a.Writer, "%#v\n", m)
					}

				} else if d == "" { // if empty line skip
					continue

				} else { // adding new data to packet buffer
					p = append(p, d)

				}

			case <-quit:

				return
			}
		}
	}()

	return
}

// OnNetError, add callback, executes on network error
func (a *Asterisk) OnNetError(c func(error)) {

	a.necb = &c
}

// send, generic send packet to Asterisk
func (a *Asterisk) send(p string) (err error) {

	p += T
	_, err = fmt.Fprintf(a.C, p)

	return
}

// generateId, generate aciton id for commands
func (a *Asterisk) generateId() (id string) {

	id = a.hostname + "-" + fmt.Sprint(a.id)
	s := sync.RWMutex{}
	s.Lock()
	defer s.Unlock()

	a.id++
	return
}

// SendAction, sends any action to AMI, if action is command, example,
// "core show channels" all output data (with newlines and tabs) will be stored in Message field CmdData,
// except end string
func (a *Asterisk) SendAction(p map[string]string, c func(Message)) {

	cmd := ""

	if v, ok := p["ActionID"]; !ok || v == "" { // if no ActionID will generate
		p["ActionID"] = a.generateId()

	}

	if c != nil {
		a.updateActionCallback(p["ActionID"], c)
	}

	for k, v := range p {
		cmd += k + ":" + v + T
	}

	if err := a.send(cmd); err != nil {
		fmt.Printf("Error %s\n", err.Error())
	}
}

// Login, login to AMI
func (a *Asterisk) Login(user string, password string) error {

	lhc := make(chan error)
	lh := func(m Message) {
		if m["Response"] == "Success" {
			lhc <- nil
		} else {
			lhc <- fmt.Errorf("%s", m["Message"])
		}
	}

	aid := a.generateId()
	l := make(map[string]string)

	a.updateActionCallback(aid, lh)

	l["Action"] = "Login"
	l["ActionID"] = aid
	l["Username"] = user
	l["Secret"] = password

	a.SendAction(l, nil)

	return <-lhc
}

// Originate, place call
func (a *Asterisk) Originate(
	channel string, exten string, context string, priority string, timeout string, callerId string,
	account string, async bool, variable map[string]string, c func(Message)) {

	aid := a.generateId()
	o := map[string]string{
		"Action":   "Originate",
		"ActionID": aid,
		"Channel":  channel,
		"Exten":    exten,
		"Context":  context,
		"Priority": priority,
		"Timeout":  timeout,
		"CallerID": callerId,
		"Account":  account,
	}

	if timeout == "" {
		o["Timeout"] = O_TMOUT
	}

	if async {
		o["Async"] = "True"
	}

	for k, v := range variable {
		o["Variable"] += k + "=" + v + ","
	}

	o["Variable"] = strings.TrimRight(o["Variable"], ",")

	if c != nil {
		a.updateActionCallback(aid, c)
	}

	a.SendAction(o, nil)
}

// OriginateApp, place call and goes to application
func (a *Asterisk) OriginateApp(
	channel string, app string, data string, timeout string, callerId string, account string, async bool,
	variable map[string]string, c func(Message)) {

	aid := a.generateId()

	o := map[string]string{
		"Action":      "Originate",
		"ActionID":    aid,
		"Channel":     channel,
		"Application": app,
		"Data":        data,
		"Timeout":     timeout,
		"CallerID":    callerId,
		"Account":     account,
	}

	if timeout == "" {
		o["Timeout"] = O_TMOUT
	}

	if async {
		o["Async"] = "True"
	}

	for k, v := range variable {
		o["Variable"] += k + "=" + v + ","
	}

	o["Variable"] = strings.TrimRight(o["Variable"], ",")

	if c != nil {
		a.updateActionCallback(aid, c)
	}

	a.SendAction(o, nil)
}

// Hangup, close a channel
func (a *Asterisk) Hangup(channel string, c func(Message)) {

	aid := a.generateId()

	h := map[string]string{
		"Action":   "Hangup",
		"ActionID": aid,
		"Channel":  channel,
	}

	if c != nil {
		a.updateActionCallback(aid, c)
	}

	a.SendAction(h, nil)
}

// Redirect, move channel to different context
func (a *Asterisk) Redirect(channel string, exten string, context string, priority string, c func(Message)) {

	aid := a.generateId()

	r := map[string]string{
		"Action":   "Redirect",
		"ActionID": aid,
		"Channel":  channel,
		"Exten":    exten,
		"Context":  context,
		"Priority": priority,
	}

	if c != nil {
		a.updateActionCallback(aid, c)
	}

	a.SendAction(r, nil)
}

// Logoff, finishing working with AMI (will close net.Conn)
func (a *Asterisk) Logoff(c func(Message)) {

	aid := a.generateId()

	l := map[string]string{
		"Action":   "Logoff",
		"ActionID": aid,
	}

	if c != nil {
		a.updateActionCallback(aid, c)
	}

	a.SendAction(l, nil)
}

// RegisterHandler, register handle function for specific Asterisk event
func (a *Asterisk) RegisterHandler(event string, c func(Message)) {

	if c != nil {
		a.updateEventCallback(event, c)
	}
}

// UnregisterHandler, remove previously registered event handler
func (a *Asterisk) UnregisterHandler(event string) {

	a.updateEventCallback(event, nil)
}
