package desktop

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
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

	home    string
	id      int
	ws      *websocket.Conn
	cmd     *exec.Cmd
	targets map[string]struct{}
}

type Request struct {
	ID     int         `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
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
	ID         string `json:"sessionId"`
	TargetInfo struct {
		Type string `json:"type"`
		ID   string `json:"targetId"`
		Url  string `json:"url"`
	} `json:"targetInfo"`
}

type ReplyMessage struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
	Params json.RawMessage `json:"params"`
}

func (c *Chrome) Open(dir, path, uri string) error {
	var err error
	args := make([]string, 0)
	args = append(args, DefaultChromeArgs...)
	args = append(args, fmt.Sprintf("--user-data-dir=%s", dir))
	args = append(args, fmt.Sprintf("--app=%s", uri))
	args = append(args, "--force-app-mode")
	args = append(args, "--kiosk")
	// args = append(args, "--app-auto-launched")
	// args = append(args, "--hide-scrollbars")
	// args = append(args, "--no-startup-window")

	c.home = uri
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

	req := c.NewRequest("Target.setDiscoverTargets", H{"discover": true})
	if err := websocket.JSON.Send(c.ws, req); err != nil {
		return err
	}

	return nil
}

func (c *Chrome) WaitClose() {
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

func (c *Chrome) NewRequestAsString(method string, params interface{}) string {
	req := c.NewRequest(method, params)
	bytes, _ := json.Marshal(req)
	return string(bytes)
}

func (c *Chrome) loop() {
	for {
		rm := ReplyMessage{}
		if err := websocket.JSON.Receive(c.ws, &rm); err != nil {
			continue
		}

		raws, _ := json.Marshal(rm)
		fmt.Printf("%s\n\n\n", string(raws))

		switch rm.Method {
		case "Target.targetCreated":
			reply := &TargetCreated{}
			if err := json.Unmarshal(rm.Params, reply); err != nil {
				continue
			}

			if reply.TargetInfo.Type == "page" {
				c.targets[reply.TargetInfo.ID] = struct{}{}
				websocket.JSON.Send(c.ws, c.NewRequest("Target.attachToTarget", H{"targetId": reply.TargetInfo.ID}))
			}

		case "Target.targetDestroyed":
			reply := &TargetDestroyed{}
			if err := json.Unmarshal(rm.Params, reply); err != nil {
				continue
			}

			delete(c.targets, reply.TargetID)
			if len(c.targets) == 0 {
				c.Kill()
				return
			}

		case "Target.attachedToTarget":
			reply := &AttachToTarget{}
			if err := json.Unmarshal(rm.Params, reply); err != nil {
				continue
			}

			if strings.HasPrefix(reply.TargetInfo.Url, "chrome://") {
				req := c.NewRequestAsString("Page.navigate", H{"url": c.home})
				websocket.JSON.Send(c.ws, c.NewRequest("Target.sendMessageToTarget", H{"message": req, "sessionId": reply.ID}))
			}

			reqs := []string{
				// c.NewRequestAsString("Page.enable", nil),
				// c.NewRequestAsString("Target.setAutoAttach", H{"autoAttach": true, "waitForDebuggerOnStart": false}),
				// c.NewRequestAsString("Network.enable", nil),
				// c.NewRequestAsString("Runtime.enable", nil),
				// c.NewRequestAsString("Security.enable", nil),
				// c.NewRequestAsString("Performance.enable", nil),
				// c.NewRequestAsString("Log.enable", nil),
			}

			for _, req := range reqs {
				websocket.JSON.Send(c.ws, c.NewRequest("Target.sendMessageToTarget", H{"message": req, "sessionId": reply.ID}))
			}

		}
	}
}

func NewChrome() *Chrome {
	return &Chrome{
		id:      1,
		targets: make(map[string]struct{}),
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
