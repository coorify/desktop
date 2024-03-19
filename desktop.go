package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/coorify/desktop/message"
	"github.com/coorify/go-value"
)

type Backend interface {
	Option() interface{}

	Start() error
	Stop(grace bool) error
}

func ChromePath() string {
	if path, ok := os.LookupEnv("COORIFY_CHROME"); ok {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
		}
	case "windows":
		paths = []string{
			os.Getenv("LocalAppData") + "/Google/Chrome/Application/chrome.exe",
			os.Getenv("ProgramFiles") + "/Google/Chrome/Application/chrome.exe",
			os.Getenv("ProgramFiles(x86)") + "/Google/Chrome/Application/chrome.exe",
			os.Getenv("LocalAppData") + "/Chromium/Application/chrome.exe",
			os.Getenv("ProgramFiles") + "/Chromium/Application/chrome.exe",
			os.Getenv("ProgramFiles(x86)") + "/Chromium/Application/chrome.exe",
			os.Getenv("ProgramFiles(x86)") + "/Microsoft/Edge/Application/msedge.exe",
			os.Getenv("ProgramFiles") + "/Microsoft/Edge/Application/msedge.exe",
		}
	case "linux":
		paths = []string{
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
		}
	}

	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		return path
	}

	return ""
}

func Run(be Backend) error {
	dir, err := os.MkdirTemp("", "coorify-desktop")
	if err != nil {
		return err
	}

	title := "Coorify Desktop Error"
	bnr := ChromePath()
	if bnr == "" {
		if message.MessageBox(title, "No Chrome/Chromium installation was found. Would you like to download and install it now?") {
			url := "https://www.google.com/chrome/"
			switch runtime.GOOS {
			case "linux":
				exec.Command("xdg-open", url).Run()
			case "darwin":
				exec.Command("open", url).Run()
			case "windows":
				r := strings.NewReplacer("&", "^&")
				exec.Command("cmd", "/c", "start", r.Replace(url)).Run()
			}
		}

		return fmt.Errorf("%s", title)
	}

	if err := be.Start(); err != nil {
		message.MessageBox(title, "Only one instance can be run")
		return err
	}

	opt := be.Option()
	host := value.MustGet(opt, "Server.Host").(string)
	port := value.MustGet(opt, "Server.Port").(int)
	if host == "" {
		host = "127.0.0.1"
	}

	uri := fmt.Sprintf("http://%s:%d", host, port)

	crm := NewChrome()
	if err := crm.Init(dir, bnr, uri); err != nil {
		return err
	}

	if err := crm.Load(uri); err != nil {
		return err
	}

	crm.WaitDone()

	if err := be.Stop(false); err != nil {
		return err
	}

	return nil
}
