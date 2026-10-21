package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Console streams a server's console through Wings: first the recent history,
// then live lines until ctx ends. It reconnects when the token expires.
func (c *Client) Console(ctx context.Context, identifier string, line func(string)) error {
	for {
		err := c.consoleOnce(ctx, identifier, line)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil && err != errTokenExpired {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
}

type wsEvent struct {
	Event string   `json:"event"`
	Args  []string `json:"args,omitempty"`
}

type tokenErr string

func (e tokenErr) Error() string { return string(e) }

const errTokenExpired = tokenErr("websocket token expired")

func (c *Client) consoleOnce(ctx context.Context, identifier string, line func(string)) error {
	var cred struct {
		Data struct {
			Token  string `json:"token"`
			Socket string `json:"socket"`
		} `json:"data"`
	}
	if err := c.client(ctx, http.MethodGet, "/servers/"+identifier+"/websocket", nil, &cred); err != nil {
		return err
	}
	h := http.Header{}
	h.Set("Origin", strings.TrimRight(c.URL, "/")) // Wings only accepts the panel's origin
	conn, _, err := websocket.Dial(ctx, cred.Data.Socket, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)
	send := func(ev wsEvent) error {
		b, _ := json.Marshal(ev)
		return conn.Write(ctx, websocket.MessageText, b)
	}
	if err := send(wsEvent{Event: "auth", Args: []string{cred.Data.Token}}); err != nil {
		return err
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var ev wsEvent
		if json.Unmarshal(data, &ev) != nil {
			continue
		}
		switch ev.Event {
		case "auth success":
			_ = send(wsEvent{Event: "send logs"})
		case "console output", "install output":
			for _, a := range ev.Args {
				line(a)
			}
		case "status":
			if len(ev.Args) > 0 {
				line("[panel] server is " + ev.Args[0])
			}
		case "token expiring", "token expired":
			return errTokenExpired
		case "jwt error", "daemon error":
			return tokenErr(strings.Join(ev.Args, " "))
		}
	}
}
