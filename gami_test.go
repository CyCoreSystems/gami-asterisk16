package gami

import (
	"bufio"
	"flag"
	"fmt"
	check "gopkg.in/check.v1"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

var integration = flag.Bool("run-integration", false, "run integration tests")

func init() {
	flag.Parse()
}

type UnitSuite struct{}

var _ = check.Suite(&UnitSuite{})

func (s *UnitSuite) TestAid(t *check.C) {
	host, err := os.Hostname()
	if err != nil {
		host = _HOST
	}
	a := NewAid()
	if a.Generate() != host+"-1" {
		t.Fail()
	}
}

func (s *UnitSuite) TestCb(t *check.C) {
	cb := cbList{
		&sync.RWMutex{},
		make(map[string]*func(Message)),
		make(map[string]bool),
	}

	tf := func(m Message) {}
	k := "test1"
	cb.set(k, &tf, true)

	if cb.f[k] == nil || cb.sd[k] != true {
		t.Fail()
	}

	f, sd := cb.get(k)
	if f != &tf || !sd {
		t.Fail()
	}

	cb.del(k)
	if _, e := cb.f[k]; e {
		t.Fail()
	}

	if _, e := cb.sd[k]; e {
		t.Fail()
	}
}

func Test(t *testing.T) {
	check.TestingT(t)
}

type IntegrationSuite struct{}

var _ = check.Suite(&IntegrationSuite{})
var a *Asterisk
var am *amock

func (s *IntegrationSuite) SetUpSuite(c *check.C) {

	if !*integration {
		c.Skip("integration tests not enabled")
		return
	}

	l, err := net.Listen("tcp", "localhost:42420")
	if err != nil {
		c.Log("Can't start Asterisk mock: ", err)
		c.Fail()
	}
	am = &amock{l}
	go am.start()
	conn, err := net.Dial("tcp", "localhost:42420")
	if err != nil {
		am.stop()
		c.Log("Can't connect to mock: ", err)
		c.Fail()
	}
	a = NewAsterisk(&conn, nil)
	a.Login("admin", "admin")
	sleep(2)
}

func (i *IntegrationSuite) TestOriginate(c *check.C) {
	o := NewOriginateApp("sip/1234", "playback", "hello-world")
	ch := make(chan Message)
	check := func(m Message) {
		ch <- m
	}
	a.Originate(o, nil, &check)
	r := <-ch
	if r["Response"] != "Error" {
		c.Fail()
	}
}

func (i *IntegrationSuite) TestRedirect(c *check.C) {
	ch := make(chan Message)
	check := func(m Message) {
		ch <- m
	}
	a.Redirect("sip/1234", "new", "1000", "1", &check)
	r := <-ch
	if r["Response"] != "Error" {
		c.Fail()
	}
}

func (i *IntegrationSuite) TestDb(c *check.C) {
	ch := make(chan Message)
	check := func(m Message) {
		ch <- m
	}
	a.RegisterHandler("DbGetResponse", &check)
	a.DbPut("test", "newkey", "1000", nil)
	a.DbGet("test", "newkey", nil)
	r := <-ch
	if r["Val"] != "1000" {
		c.Fail()
	}
	a.UnregisterHandler("DbGetResponse")
	a.DbDelTree("test", "", nil)
	a.DbGet("test", "newkey", &check)
	r = <-ch
	if r["Response"] != "Error" {
		c.Fail()
	}
}

func (i *IntegrationSuite) TestConferenceCreate(c *check.C) {
	ch := make(chan Message)
	check := func(m Message) {
		ch <- m
	}

	cnt := 5

	o := NewOriginateApp("fakeconference/conf1", "", "")
	for i := 0; i < cnt; i++ {
		a.Originate(o, nil, &check)
		r := <-ch
		if r["Response"] != "Success" {
			c.Fail()
		}
	}

	mbrs, _ := a.GetConfbridgeList("conf1")
	if len(mbrs) != cnt {
		c.Fail()
	}
}

func (i *IntegrationSuite) TestConferenceDestroy(c *check.C) {
	ch := make(chan Message)
	check := func(m Message) {
		ch <- m
	}

	cnt := 3

	o := NewOriginateApp("fakeconference/conf2", "", "")
	for i := 0; i < cnt; i++ {
		a.Originate(o, nil, &check)
		r := <-ch
		if r["Response"] != "Success" {
			c.Fail()
		}
	}

	mbrs, _ := a.GetConfbridgeList("conf2")
	if len(mbrs) != cnt {
		c.Fail()
	}

	a.ConfbridgeKick("conf2", "all", nil)
	mbrs, _ = a.GetConfbridgeList("conf2")
	if len(mbrs) != 0 {
		c.Fail()
	}
}

func (i *IntegrationSuite) TestUserEvent(c *check.C) {
	ch := make(chan Message)
	check := func(m Message) {
		ch <- m
	}
	a.RegisterHandler("UserEvent", &check)
	a.UserEvent("TestEvent", map[string]string{"Key1": "Val1", "Key2": "Val2"}, nil)

	r := <-ch
	if r["UserEvent"] != "TestEvent" || r["Key1"] != "Val1" || r["Key2"] != "Val2" {
		c.Fail()
	}
}

func (s *IntegrationSuite) TearDownSuite(c *check.C) {
	if *integration {
		a.Logoff()
		am.stop()
	}
}

type amock struct {
	ln net.Listener
}

func (a *amock) start() {
	for {
		con, err := a.ln.Accept()
		if err != nil {
			break
		}
		handleConnection(bufio.NewReadWriter(bufio.NewReader(con), bufio.NewWriter(con)))
	}
}

func (a *amock) stop() {
	a.ln.Close()
}

func handleConnection(rw *bufio.ReadWriter) {

	m := Message{}
	db := make(map[string]map[string]string)
	confs := make(map[string]int)

	for {
		b, _, err := rw.ReadLine()
		if err != nil {
			break
		}
		if len(b) == 0 {
			r := Message{
				"ActionID": m["ActionID"],
			}
			switch m["Action"] {
			case "Login":
				r["Response"] = "Success"
			case "Originate":
				if strings.HasPrefix(m["Channel"], "fakeconference/") {
					arr := strings.Split(m["Channel"], "/")
					confs[arr[1]]++
					r["Response"] = "Success"
				} else {
					r["Response"] = "Error"
				}
			case "Redirect":
				r["Response"] = "Error"
			case "DBPut":
				r["Response"] = "Success"
				fam := m["Family"]
				if _, ok := db[fam]; !ok {
					db[fam] = make(map[string]string)
				}
				db[fam][m["Key"]] = m["Value"]
			case "DBDelTree":
				r["Response"] = "Success"
				delete(db, m["Family"])
			case "DBGet":
				ok := false
				var val string
				if val, ok = db[m["Family"]][m["Key"]]; ok {
					r["Response"] = "Success"
					writeMessage(rw, r)
					r = Message{
						"Val":    val,
						"Event":  "DbGetResponse",
						"Family": m["Family"],
						"Key":    m["Key"],
					}
				} else {
					r["Response"] = "Error"
				}
			case "ConfbridgeList":
				if _, ok := confs[m["Conference"]]; ok {
					for i := 0; i < confs[m["Conference"]]; i++ {
						mem := Message{
							"Member":   fmt.Sprint(i),
							"Channel":  "sip/" + fmt.Sprint(i),
							"ActionID": m["ActionID"],
						}
						writeMessage(rw, mem)
					}
					r["Event"] = "ConfbridgeListComplete"
					r["EventList"] = "Complete"
				} else {
					r["Response"] = "Error"
				}
			case "ConfbridgeKick":
				if _, ok := confs[m["Conference"]]; ok {
					delete(confs, m["Conference"])
					r["Response"] = "Success"
				} else {
					r["Response"] = "Error"
				}
			case "UserEvent":
				r = m
				r["Event"] = "UserEvent"
			default:
				continue
			}
			writeMessage(rw, r)
			m = Message{}
		} else {
			arr := strings.Split(string(b), ":")
			if len(arr) < 2 {
				m["Unknown"] += arr[0]
			} else {
				m[arr[0]] = arr[1]
			}
		}
	}
}

func writeMessage(w *bufio.ReadWriter, m Message) {
	raw := ""
	for k, v := range m {
		raw += k + ":" + v + "\r\n"
	}
	raw += "\r\n"
	w.WriteString(raw)
	w.Flush()
}

func sleep(s int64) {
	time.Sleep(time.Duration(s) * time.Second)
}
