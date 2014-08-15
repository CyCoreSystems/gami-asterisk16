/*
	Asterisk Gateway Interface support

	Usage:

		a := agi.NewAgi()
		r := a.Answer()
		checkErr(r.Err)
		...
		r = a.Noop("Hello New Chan")
		...
		r = a.SayAlpha("Hi")
		...
		r, status := a.ChannelStatus()
		...
		r.Hangup()

*/
package agi

import (
	"bufio"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// supported commands
const (
	Answer        Cmd = "ANSWER"
	Noop          Cmd = "NOOP"
	Hangup        Cmd = "HANGUP"
	ChannelStatus Cmd = "CHANNEL STATUS"
	SayAlpha      Cmd = "SAY ALPHA"
	SayDigits     Cmd = "SAY DIGITS"
	SayNumber     Cmd = "SAY NUMBER"
	SayDate       Cmd = "SAY DATE"
	SayTime       Cmd = "SAY TIME"
	SayDateTime   Cmd = "SAY DATETIME"
	WaitForDigit  Cmd = "WAIT FOR DIGIT"
	GetVariable   Cmd = "GET VARIABLE"
	SetVariable   Cmd = "SET VARIABLE"
	SetCallerId   Cmd = "SET CALLERID"
	SetContext    Cmd = "SET CONTEXT"
	SetExtension  Cmd = "SET EXTENSION"
	SetPripority  Cmd = "SET PRIORITY"
	SendText      Cmd = "SEND TEXT"
	DBDel         Cmd = "DATABASE DEL"
	DBDelTree     Cmd = "DATABASE DELTREE"
	DBGet         Cmd = "DATABASE GET"
	DBPut         Cmd = "DATABASE PUT"
)

var (
	_RespRe *regexp.Regexp = regexp.MustCompile("^(\\d*)\\s*(.*)")
	_VarRe  *regexp.Regexp = regexp.MustCompile("^(\\d*)\\s*(.*)\\s*\\((\\S+)\\)")
)

// AGI command
type Cmd string

// response struct
type Resp struct {
	Payload string // response body
	Code    int    // response code
	Err     error  // error if exist
}

func (r Resp) String() string {
	res := "{ Code: " + strconv.Itoa(r.Code) + " Payload: " + r.Payload
	if r.Err != nil {
		res += " Error: " + r.Err.Error()
	}
	return res + " }"
}

// main agi struct
type Agi struct {
	Env map[string]string // AGI environment variables
	in  *bufio.Reader
	out *bufio.Writer
}

// factory function
func NewAgi() *Agi {
	a := &Agi{
		Env: make(map[string]string),
		in:  bufio.NewReader(os.Stdin),
		out: bufio.NewWriter(os.Stdout),
	}
	a.readEnv()
	return a
}

// answer channel
func (a *Agi) Answer() *Resp {
	return a.exec(Answer)
}

// return channel status
func (a *Agi) ChannelStatus() (*Resp, int) {
	r := a.exec(ChannelStatus)
	statusRe := regexp.MustCompile("^\\d+\\s*result=(\\d+)")
	if r.Err != nil {
		return r, -1
	}

	groups := statusRe.FindStringSubmatch(r.Payload)
	status, _ := strconv.Atoi(groups[1])
	return r, status
}

// NOOP
func (a *Agi) Noop(args ...string) *Resp {
	return a.exec(Noop, args...)
}

// handup channel
func (a *Agi) Hangup() *Resp {
	return a.exec(Hangup)
}

// wait for digit on channel, timeout in ms
func (a *Agi) WaitForDigit(wait int) (*Resp, int) {
	resp := a.exec(WaitForDigit, strconv.Itoa(wait))
	if resp.Err != nil {
		return resp, -1
	}

	pattern := regexp.MustCompile("^\\d+\\s*result=(\\d+)")
	groups := pattern.FindStringSubmatch(resp.Payload)
	ascii, _ := strconv.Atoi(groups[1])
	digit, _ := strconv.Atoi(string(ascii))
	return resp, digit
}

// say datetime
func (a *Agi) SayDateTime(unixtime int64, format string) *Resp {
	return a.exec(SayDateTime, strconv.FormatInt(unixtime, 10), format)
}

// say digits
func (a *Agi) SayDigits(num int) *Resp {
	return a.exec(SayDigits, strconv.Itoa(num))
}

// say letters
func (a *Agi) SayAlpha(str string) *Resp {
	return a.exec(SayAlpha, str)
}

// get channel variable
func (a *Agi) GetVariable(name string) (*Resp, string) {
	return a.readVarFromResp(a.exec(GetVariable, name))
}

// set channel variable
func (a *Agi) SetVariable(name, val string) *Resp {
	return a.exec(SetVariable, name, val)
}

// set channel context
func (a *Agi) SetContext(ctx string) *Resp {
	return a.exec(SetContext, ctx)
}

// set channel extension
func (a *Agi) SetExtension(ext string) *Resp {
	return a.exec(SetExtension, ext)
}

// set channel priority for exten
func (a *Agi) SetPripority(pr string) *Resp {
	return a.exec(SetPripority, pr)
}

// basic command execute
func (a *Agi) Exec(cmd Cmd, args ...string) *Resp {
	return a.exec(cmd, args...)
}

// send text to channel, if support
func (a *Agi) SendText(text string) *Resp {
	return a.exec(SendText, text)
}

// delete key from DB
func (a *Agi) DBDel(familiy, key string) *Resp {
	return a.exec(DBDel, familiy, key)
}

// delete DB family
func (a *Agi) DBDelTree(family string) *Resp {
	return a.exec(DBDelTree, family)
}

// save value to DB
func (a *Agi) DBPut(family, key, val string) *Resp {
	return a.exec(DBPut, family, key, val)
}

// get value from DB
func (a *Agi) DBGet(family, key string) (*Resp, string) {
	return a.readVarFromResp(a.exec(DBGet, family, key))
}

func (a *Agi) exec(cmd Cmd, args ...string) *Resp {
	arg := " "
	for _, v := range args {
		arg += v + " "
	}
	a.out.WriteString(string(cmd) + arg + "\n")
	a.out.Flush()

	return a.readResponse()
}

func (a *Agi) readEnv() error {
	for {
		line, err := a.in.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			break
		}
		lines := strings.Split(line, ":")
		a.Env[lines[0]] = lines[1]
	}
	return nil
}

func (a *Agi) readResponse() *Resp {
	resp := &Resp{}
	line, err := a.in.ReadString('\n')
	if err != nil {
		resp.Err = err
		return resp
	}

	line = strings.TrimSpace(line)

	if _RespRe.MatchString(line) {
		groups := _RespRe.FindStringSubmatch(line)
		code, _ := strconv.Atoi(groups[1])
		resp.Code = code
		resp.Payload = line
		switch code {
		case 520:
			return a.read520(resp)
		case 200:
			return resp
		case 510:
			resp.Err = errors.New("Unknown command")
			return resp
		default:
			resp.Err = errors.New("Unknown code")
			return resp
		}
	} else {
		resp.Err = errors.New("Unknown reponse: " + line)
	}
	return resp
}

func (a *Agi) read520(resp *Resp) *Resp {

	msg := resp.Payload + "\n"
	resp.Payload = ""

	for {
		line, err := a.in.ReadString('\n')
		if err != nil {
			resp.Err = err
		}
		msg += line + "\n"
		if _RespRe.MatchString(line) {
			resp.Err = errors.New(msg)
			return resp
		}
	}
}

func (a *Agi) readVarFromResp(r *Resp) (*Resp, string) {
	if r.Err != nil {
		return r, ""
	}
	groups := _VarRe.FindStringSubmatch(r.Payload)
	if len(groups) != 4 {
		r.Err = errors.New("No such variable")
		return r, ""
	}
	return r, groups[3]
}
