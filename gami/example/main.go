package main

import (
	gami "code.google.com/p/gami"
	"flag"
	"fmt"
	"log"
	"net"
)

var (
	host, user, password string
	port                 int
	debug                bool = false
)

func init() {
	flag.IntVar(&port, "port", 5038, "AMI port")
	flag.StringVar(&host, "host", "localhost", "AMI host")
	flag.StringVar(&user, "user", "admin", "AMI user")
	flag.StringVar(&password, "password", "admin", "AMI secret")
	flag.Parse()
}

func main() {
	c, err := net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))

	if err != nil {
		log.Fatal(err)
	}

	defer c.Close()

	g := gami.NewAsterisk(&c, nil)

	err = g.Login(user, password)

	if err != nil {
		log.Fatal(err)
	}

	var s string

END:
	for {
		printMenu()

		fmt.Scanf("%s\n", &s)

		switch s {
		case "q":
			break END
		case "p":
			ping(g)
		case "d":
			debug = !debug
			toggleDebug(g)
		case "o":
			originate(g)
		case "l":
			list(g)
		default:
			printMenu()
		}

	}

	g.Logoff()
}

func list(a *gami.Asterisk) {
	fmt.Println("Enter conference name: ")
	var conf string
	fmt.Scanf("%s\n", &conf)

	ml, err := a.GetConfbridgeList(conf)

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(ml)
}

func originate(a *gami.Asterisk) {

	fmt.Println("Enter channel: ")
	var ch string
	fmt.Scanf("%s\n", &ch)

	o := gami.NewOriginateApp(ch, "playback", "hello-world")
	var err error

	if !debug {
		cb := func(m gami.Message) {
			fmt.Println(m)
		}
		err = a.Originate(o, nil, &cb)
	} else {
		err = a.Originate(o, nil, nil)
	}

	if err != nil {
		log.Fatal(err)
	}
}

func toggleDebug(a *gami.Asterisk) {
	if debug {
		fmt.Println("Enabling debugging AMI events")
		dh := func(m gami.Message) {
			fmt.Println(m)
		}
		a.DefaultHandler(&dh)
	} else {
		fmt.Println("Disabling debugging AMI events")
		a.DefaultHandler(nil)
	}

}

func ping(a *gami.Asterisk) {
	m := gami.Message{"Action": "Ping"}
	cb := func(m gami.Message) {
		fmt.Println(m)
	}
	err := a.SendAction(m, &cb)
	if err != nil {
		log.Fatal(err)
	}
}

func printMenu() {
	fmt.Println("Usage:")
	fmt.Println(" d -> toggle debug events")
	fmt.Println(" o -> originate to channel")
	fmt.Println(" l -> list conference participants")
	fmt.Println(" p -> to ping")
	fmt.Println(" q -> to quit")
}
