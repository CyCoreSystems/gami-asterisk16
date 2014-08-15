package agi

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

var (
	in  = &bytes.Buffer{}
	out = &bytes.Buffer{}
	a   = Agi{
		in:  bufio.NewReader(in),
		out: bufio.NewWriter(out),
		Env: make(map[string]string),
	}
)

func TestReadEnv(t *testing.T) {
	reset()
	tmap := map[string]string{"key1": "val1", "key2": "val2", "key3": "val3"}
	for k, v := range tmap {
		in.WriteString(k + ":" + v + "\n")
	}
	in.WriteString("\n")
	a.readEnv()

	for k, v := range tmap {
		if val, ok := a.Env[k]; !ok || val != v {
			t.Fail()
		}
	}
}

func TestChannelStatus(t *testing.T) {
	reset()
	in.WriteString("200 result=4\n")
	r, s := a.ChannelStatus()

	if r.Code != 200 || s != 4 {
		t.Fail()
	}
}

func TestDBPut(t *testing.T) {
	reset()
	a.DBPut("newfam", "mykey", "someval")
	s, _ := out.ReadString('\n')
	if strings.TrimSpace(s) != "DATABASE PUT newfam mykey someval" {
		t.Fail()
	}
}

func TestDBGet(t *testing.T) {
	reset()
	in.WriteString("200 result=1\n")
	r, val := a.DBGet("fam1", "key")
	if val != "" || r.Err == nil {
		t.Fail()
	}
}

func reset() {
	a.Env = make(map[string]string)
	in.Reset()
	out.Reset()
}
