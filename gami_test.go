package gami

import (
	"os"
	"sync"
	"testing"
)

func TestAid(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		host = _HOST
	}
	a := NewAid()
	if a.Generate() != host+"-1" {
		t.Fail()
	}
}

func TestCb(t *testing.T) {
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
