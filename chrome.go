package desktop

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sync"

	"golang.org/x/net/websocket"
)

var DefaultChromeArgs = []string{
	"--disable-background-networking",
	"--disable-background-timer-throttling",
	"--disable-backgrounding-occluded-windows",
	"--disable-breakpad",
	"--disable-client-side-phishing-detection",
	"--disable-default-apps",
	"--disable-dev-shm-usage",
	"--disable-infobars",
	"--disable-extensions",
	"--disable-features=site-per-process",
	"--disable-hang-monitor",
	"--disable-ipc-flooding-protection",
	"--disable-popup-blocking",
	"--disable-prompt-on-repost",
	"--disable-renderer-backgrounding",
	"--disable-sync",
	"--disable-translate",
	"--disable-windows10-custom-titlebar",
	"--metrics-recording-only",
	"--no-first-run",
	"--no-default-browser-check",
	"--safebrowsing-disable-auto-update",
	// "--enable-automation",
	"--password-store=basic",
	"--use-mock-keychain",
	"--remote-allow-origins=*",
	"--remote-debugging-port=0",
}

type H map[string]interface{}

type Chrome struct {
	sync.Mutex

	id      int
	ws      *websocket.Conn
	cmd     *exec.Cmd
	pending map[interface{}]chan json.RawMessage

	Target  string
	Session string
}

type Request struct {
	ID     int         `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
}

type Reply struct {
	ID      int
	Method  string
	Payload interface{}
}

type TargetCreated struct {
	TargetInfo struct {
		Type string `json:"type"`
		ID   string `json:"targetId"`
	} `json:"targetInfo"`
}

type TargetDestroyed struct {
	TargetID string `json:"targetId"`
}

type AttachToTarget struct {
	ID string `json:"sessionId"`
}

type ReplyMessage struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
	Params json.RawMessage `json:"params"`
}

func (c *Chrome) Init(dir, path, uri string) error {
	var err error
	args := append(DefaultChromeArgs, fmt.Sprintf("--user-data-dir=%s", dir))
	args = append(args, fmt.Sprintf("--app=%s", uri))

	c.cmd = exec.Command(path, args...)

	reader, err := c.cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := c.cmd.Start(); err != nil {
		return err
	}

	c.ws, err = NewWebsocket(reader)
	if err != nil {
		c.Kill()
		return err
	}

	go c.loop()

	created := &TargetCreated{}
	if err := c.Request("Target.setDiscoverTargets", H{"discover": true}, "Target.targetCreated", created); err != nil {
		return err
	}
	c.Target = created.TargetInfo.ID

	session := &AttachToTarget{}
	if err := c.Request("Target.attachToTarget", H{"targetId": c.Target}, "Target.attachToTarget", session); err != nil {
		return err
	}
	c.Session = session.ID

	// for method, args := range map[string]H{
	// 	"Page.enable":          nil,
	// 	"Target.setAutoAttach": {"autoAttach": true, "waitForDebuggerOnStart": false},
	// 	"Network.enable":       nil,
	// 	"Runtime.enable":       nil,
	// 	"Security.enable":      nil,
	// 	"Performance.enable":   nil,
	// 	"Log.enable":           nil,
	// } {
	// 	if err := c.Request(method, args, "", nil); err != nil {
	// 		c.Kill()
	// 		c.cmd.Wait()
	// 		return err
	// 	}
	// }

	return nil
}

func (c *Chrome) Load(uri string) error {
	return c.Request("Page.navigate", H{"url": uri}, "", nil)
}

func (c *Chrome) WaitDone() {
	done := make(chan struct{})

	go func() {
		c.cmd.Wait()
		close(done)
	}()

	<-done
}

func (c *Chrome) Kill() error {
	if c.ws != nil {
		if err := c.ws.Close(); err != nil {
			return err
		}
	}

	if state := c.cmd.ProcessState; state == nil || !state.Exited() {
		return c.cmd.Process.Kill()
	}

	return nil
}

func (c *Chrome) NewRequest(method string, params interface{}) *Request {
	id := c.id
	c.id += 1

	return &Request{
		ID:     id,
		Method: method,
		Params: params,
	}
}

func (c *Chrome) NewReply(method string, reply interface{}) *Reply {
	return &Reply{
		Method:  method,
		Payload: reply,
	}
}

func (c *Chrome) Request(reqMethod string, params interface{}, repMethod string, reply interface{}) error {
	req := c.NewRequest(reqMethod, params)
	rep := c.NewReply(repMethod, reply)
	resc := make(chan json.RawMessage)

	c.Lock()
	if reqMethod == repMethod {
		rep.ID = req.ID
		c.pending[rep.ID] = resc
	} else {
		req.ID = 0
		if rep.Method != "" {
			c.pending[rep.Method] = resc
		}
	}
	c.Unlock()

	fmt.Printf("%#v\n", req)
	if err := websocket.JSON.Send(c.ws, req); err != nil {
		return err
	}

	if rep.Method != "" && rep.Payload != nil {
		raw := <-resc
		close(resc)
		if err := json.Unmarshal(raw, rep.Payload); err != nil {
			return err
		}
	}

	return nil
}

func (c *Chrome) loop() {
	for {
		rm := ReplyMessage{}
		if err := websocket.JSON.Receive(c.ws, &rm); err != nil {
			return
		}

		c.Lock()
		resc, ok := c.pending[rm.Method]
		if ok {
			delete(c.pending, rm.Method)
			if rm.Params != nil {
				resc <- rm.Params
			} else if rm.Result != nil {
				resc <- rm.Result
			}
		}
		resc, ok = c.pending[rm.ID]
		if ok {
			delete(c.pending, rm.ID)
			if rm.Params != nil {
				resc <- rm.Params
			} else if rm.Result != nil {
				resc <- rm.Result
			}
		}

		c.Unlock()

		raws, _ := json.Marshal(rm)
		fmt.Printf("%s\n", string(raws))

		if rm.Method == "Target.targetDestroyed" {
			params := &TargetDestroyed{}
			json.Unmarshal(rm.Params, &params)
			if params.TargetID == c.Target {
				c.Kill()
				return
			}
		}

	}
}

func NewChrome() *Chrome {
	return &Chrome{
		id:      1,
		pending: make(map[interface{}]chan json.RawMessage),
	}
}

func NewWebsocket(reader io.ReadCloser) (*websocket.Conn, error) {
	var wsUrl string

	re := regexp.MustCompile(`^DevTools listening on (ws://.*?)\r?\n$`)
	br := bufio.NewReader(reader)
	for {
		if line, err := br.ReadString('\n'); err != nil {
			reader.Close()
			return nil, err
		} else if m := re.FindStringSubmatch(line); m != nil {
			go io.Copy(io.Discard, br)
			wsUrl = m[1]
			break
		}
	}

	return websocket.Dial(wsUrl, "", "http://127.0.0.1")
}
