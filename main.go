// Case Studio — a self-contained, fully offline phone-case designer.
// The whole app (HTML/JS/CSS) is embedded in this binary; it serves itself on
// localhost only and opens your default browser. No network access is needed.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"
)

//go:embed index.html
var page []byte

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("Could not open a browser automatically; open this address yourself:", url)
	}
}

func main() {
	port := flag.Int("port", 0, "port to listen on (0 = pick a free one)")
	noOpen := flag.Bool("no-browser", false, "do not open a browser window")
	keep := flag.Bool("keep-alive", false, "keep running after the browser tab is closed")
	flag.Parse()

	// Bind to loopback only: nothing outside this computer can reach it.
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(*port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot start:", err)
		os.Exit(1)
	}

	var lastPing, byeAt atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(page)
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		lastPing.Store(time.Now().UnixNano())
		byeAt.Store(0)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/bye", func(w http.ResponseWriter, r *http.Request) {
		byeAt.Store(time.Now().UnixNano())
		w.WriteHeader(http.StatusNoContent)
	})

	url := "http://" + ln.Addr().String() + "/"
	fmt.Println("Case Studio is running at", url)
	if !*keep {
		fmt.Println("It will exit by itself shortly after you close the browser tab (Ctrl+C also works).")
	}

	// Exit once the tab is closed: a "bye" that is not followed by a new ping
	// within a few seconds (a reload sends bye, then pings again), or no pings
	// at all for two minutes after the page had connected (browsers throttle
	// timers in hidden tabs, hence the generous limit).
	if !*keep {
		go func() {
			for range time.Tick(time.Second) {
				lp, bye := lastPing.Load(), byeAt.Load()
				if lp == 0 {
					continue
				}
				now := time.Now().UnixNano()
				if (bye != 0 && now-bye > 4*int64(time.Second)) || now-lp > 120*int64(time.Second) {
					fmt.Println("Browser tab closed — bye!")
					os.Exit(0)
				}
			}
		}()
	}

	if !*noOpen {
		go openBrowser(url)
	}
	if err := http.Serve(ln, mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
