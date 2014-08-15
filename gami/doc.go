/*
 Package gami implements simple Asterisk Manager Interface library.

 It's not handle any network layer, just parsing or creating packets
 and runs callback for it (if registered).

 Start working:

  conn, err := net.Dial("tcp", "astserver:5038")
  if err != nil {
      // error handling
  }

  var a gami.Asterisk
  a = gami.NewAsterisk(&conn, nil) // 2nd parameter network error callback
  err = a.Login("user", "password") // will block until receive response
  if err != nil {
    // login error handling
  }

 Placing simple command:

  ping := gami.Message{"Action":"Ping"} // ActionID will be overwritten
  pingCallback := func(m Message) {
    if m["Message"] == "Pong" {
      fmt.Println("Hurray!")
    }
  }
  a.SendAction(ping, &pingCallback) // callback will be automatically executed and deleted

 Placing a call:

  o := gami.NewOriginateApp("SIP/1234", "Playback", "hello-world")
  a.Originate(o, nil, nil) // 2nd parameter - variables passed to channel, 3rd - callback

 Event handlers:

  hangupHandler := func(m gami.Message) {
    fmt.Printf("Hangup event received for channel %s\n", m["Channel"])
  }

  a.RegisterHandler("Hangup", &hangupHandler)
  ...
  a.UnregisterHandler("Hangup")

 Default handler:

  This handler will execute for each message received from Asterisk, useful for debugging.

  dh := func(m gami.Message) {
    fmt.Println(m)
  }
  a.DefaultHandler(&dh)

 Multi-message handlers:

  Some actions (CoreShowChannels example) has multi-message output. For this point need to use
  "self-delete" callbacks.

  // this callback will run but never be deleted until own
  cscf := func() func(gami.Message) { // using closure for storing data
    ml := []gami.Message{}

    return func(m gami.Message) {
      ml = append(ml, m)
      if m["EventList"] == "Complete" { // got multi-message end
        // doing something
        fmt.Println("Destroying self...")
        a.DelCallback(m) // when finished must be removed
        }
    }
  }()

  m := gami.Message{"Action": "CoreShowChannels"}
  a.HoldCallbackAction(m, &cscf)

 Finishing:

  a.Logoff() // if a created with network error callback it will be executed

*/
package gami

// Author: Vasiliy Kovalenko <dev.boot@gmail.com>
