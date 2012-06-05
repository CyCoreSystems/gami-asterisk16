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

 Checking if login successful (package not auto logins!):

  eh := make(chan error)
  handleLogin := func(m gami.Message) {
      if m["Response"] == "Success" { eh <- nil } else { eh <- fmt.Errorf("%#v\n", m) }
  }

  a.Login("username", "password", handleLogin)
  loginErr := <- eh
  if loginErr != nil { <error handling> }

 Placing simple command:

  ping := map[string]string { "Action":"Ping", }
  a.SendAction(ping, nil)

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
//  - implement socket error handling
//  - cheking if logined to server

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
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
	eventHandlers map[string]func(Message) // map for event callbacks
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

// Handle, preparing gami for working with Asterisk
func (a *Asterisk) Handle() (err error) {

	a.ch = make(chan string, 20)
	a.callbacks = make(map[string]func(Message))
	a.eventHandlers = make(map[string]func(Message))

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
	// handler channel (skipping errors)
	go func() {
		for {
			data, err := r.ReadString('\n')
			a.ch <- data

			if err != nil {
				quit <- 1
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
						f(m)
						delete(a.callbacks, m["ActionID"])
					}

					if v, vok := m["Event"]; vok {
						if f, fok := a.eventHandlers[v]; fok {
							f(m)
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

// send, generic send packet to Asterisk
func (a *Asterisk) send(p string) (err error) {

	p += T
	_, err = fmt.Fprintf(a.C, p)
    
    if err != nil {
        panic(err)
    }

	return
}

// generateId, generate aciton id for commands
func (a *Asterisk) generateId() (id string) {

	id = a.hostname + "-" + fmt.Sprint(a.id)
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

	if _, ok := a.callbacks[p["ActionID"]]; !ok && c != nil { // if no callback and argument callback
		a.callbacks[p["ActionID"]] = c // not nil will add to execute (not replace exists)
	}

	for k, v := range p {

		cmd += k + ":" + v + T
	}

	if err := a.send(cmd); err != nil {
		fmt.Printf("Error %s\n", err.Error())
	}
}

// Login, login to AMI
func (a *Asterisk) Login(user string, password string, c func(Message)) {

	aid := a.generateId()
	l := make(map[string]string)

	if c != nil {
		a.callbacks[aid] = c
	}

	l["Action"] = "Login"
	l["ActionID"] = aid
	l["Username"] = user
	l["Secret"] = password

	a.SendAction(l, nil)
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
		a.callbacks[o["ActionID"]] = c
	}

	a.SendAction(o, nil)
}

// OriginateApp, place call and goes to application
func (a *Asterisk) OriginateApp(
	channel string, app string, data string, timeout string, callerId string, account string, async bool,
	variable map[string]string, c func(Message)) {

	o := map[string]string{
		"Action":      "Originate",
		"ActionID":    a.generateId(),
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
		a.callbacks[o["ActionID"]] = c
	}

	a.SendAction(o, nil)
}

// Hangup, close a channel
func (a *Asterisk) Hangup(channel string, c func(Message)) {

	h := map[string]string{
		"Action":   "Hangup",
		"ActionID": a.generateId(),
		"Channel":  channel,
	}

	if c != nil {
		a.callbacks[h["ActionID"]] = c
	}

	a.SendAction(h, nil)
}

// Redirect, move channel to different context
func (a *Asterisk) Redirect(channel string, exten string, context string, priority string, c func(Message)) {

	r := map[string]string{
		"Action":   "Redirect",
		"ActionID": a.generateId(),
		"Channel":  channel,
		"Exten":    exten,
		"Context":  context,
		"Priority": priority,
	}

	if c != nil {
		a.callbacks[r["ActionID"]] = c
	}

	a.SendAction(r, nil)
}

// Logoff, finishing working with AMI (will close net.Conn)
func (a *Asterisk) Logoff(c func(Message)) {

	l := map[string]string{
		"Action":   "Logoff",
		"ActionID": a.generateId(),
	}

	if c != nil {
		a.callbacks[l["ActionID"]] = c
	}

	a.SendAction(l, nil)
}

// RegisterHandler, register handle function for specific Asterisk event
func (a *Asterisk) RegisterHandler(event string, c func(Message)) {

	a.eventHandlers[event] = c
}

// UnregisterHandler, remove previously registered event handler
func (a *Asterisk) UnregisterHandler(event string) {

	delete(a.eventHandlers, event)
}
