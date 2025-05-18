package handler

import (
	//"errors"

	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"go.uber.org/zap"

	"github.com/avkiller/caddy-trojan/app"
	"github.com/avkiller/caddy-trojan/pkgs/trojan"
	"github.com/avkiller/caddy-trojan/pkgs/websocket"
	"github.com/avkiller/caddy-trojan/pkgs/x"
)

func init() {
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterHandlerDirective("trojan", func(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
		m := &Handler{}
		err := m.UnmarshalCaddyfile(h.Dispenser)
		return m, err
	})
}

// Handler implements an HTTP handler that ...
type Handler struct {
	ProxyName string `json:"proxy_name,omitempty"`
	WebSocket bool   `json:"websocket,omitempty"`
	Connect   bool   `json:"connect_method,omitempty"`
	Verbose   bool   `json:"verbose,omitempty"`

	upstream app.Upstream
	proxy    app.Proxy
	logger   *zap.Logger
	upgrader websocket.Upgrader
}

// CaddyModule returns the Caddy module information.
func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.trojan",
		New: func() caddy.Module { return new(Handler) },
	}
}

// Provision implements caddy.Provisioner.
func (m *Handler) Provision(ctx caddy.Context) error {
	m.logger = ctx.Logger(m)
	if _, err := ctx.AppIfConfigured(app.CaddyAppID); err != nil {
		return fmt.Errorf("trojan handler configure error: %w", err)
	}
	mod, err := ctx.App(app.CaddyAppID)
	if err != nil {
		return err
	}
	app := mod.(*app.App)
	m.upstream = app.GetUpstream()
	if m.ProxyName == "" {
		m.proxy = app.GetProxy()
		return nil
	}
	var ok bool
	m.proxy, ok = app.GetProxyByName(m.ProxyName)
	if !ok {
		return fmt.Errorf("proxy name: %v does not exist", m.ProxyName)
	}
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (m *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// trojan over http2/http3
	// use CONNECT method, put trojan header as Proxy-Authorization
	if m.Connect && r.Method == http.MethodConnect {
		// handle trojan over http2/http3
		if r.ProtoMajor == 1 {
			return next.ServeHTTP(w, r)
		}
		auth := strings.TrimPrefix(r.Header.Get("Proxy-Authorization"), "Basic ")
		if len(auth) != trojan.HeaderLen {
			return next.ServeHTTP(w, r)
		}
		if ok := m.upstream.Validate(auth); !ok {
			return next.ServeHTTP(w, r)
		}
		if m.Verbose {
			m.logger.Info(fmt.Sprintf("handle trojan http%d from %v", r.ProtoMajor, r.RemoteAddr))
		}

		nr, nw, err := trojan.HandleWithDialer(r.Body, NewFlushWriter(w), m.proxy)
		if err != nil {
			m.logger.Error(fmt.Sprintf("handle http%d error: %v", r.ProtoMajor, err))
		}
		m.upstream.Consume(auth, nr, nw)
		return nil
	}

	// handle websocket
	if m.WebSocket && websocket.IsWebSocketUpgrade(r) {
		conn, err := m.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return err
		}

		c := websocket.NewConn(conn)
		defer c.Close()

		b := [trojan.HeaderLen + 2]byte{}
		if _, err := io.ReadFull(c, b[:]); err != nil {
			m.logger.Error(fmt.Sprintf("read trojan header error: %v", err))
			return nil
		}
		if ok := m.upstream.Validate(x.ByteSliceToString(b[:trojan.HeaderLen])); !ok {
			return nil
		}
		if m.Verbose {
			m.logger.Info(fmt.Sprintf("handle trojan websocket.Conn from %v", r.RemoteAddr))
		}

		nr, nw, err := trojan.HandleWithDialer(io.Reader(c), io.Writer(c), m.proxy)
		if err != nil {
			m.logger.Error(fmt.Sprintf("handle websocket error: %v", err))
		}
		m.upstream.Consume(x.ByteSliceToString(b[:trojan.HeaderLen]), nr, nw)
		return nil
	}

	return next.ServeHTTP(w, r)
}

// UnmarshalCaddyfile unmarshals Caddyfile tokens into h.
func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	if !d.Next() {
		return d.ArgErr()
	}
	args := d.RemainingArgs()
	if len(args) > 0 {
		return d.ArgErr()
	}
	for nesting := d.Nesting(); d.NextBlock(nesting); {
		subdirective := d.Val()
		switch subdirective {
		case "websocket":
			if h.WebSocket {
				return d.Err("only one websocket is not allowed")
			}
			h.WebSocket = true
		case "connect_method":
			if h.Connect {
				return d.Err("only one connect_method is not allowed")
			}
			h.Connect = true
		case "proxy_name":
			if !d.Args(&h.ProxyName) {
				return d.ArgErr()
			}
		case "verbose":
			if h.Verbose {
				return d.Err("only one verbose is not allowed")
			}
			h.Verbose = true
		}
	}
	return nil
}

// Interface guards
var (
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
	_ caddyfile.Unmarshaler       = (*Handler)(nil)
)

type FlushWriter struct {
	Writer  io.Writer
	Flusher http.Flusher
}

func NewFlushWriter(w http.ResponseWriter) *FlushWriter {
	return &FlushWriter{
		Writer:  w,
		Flusher: w.(http.Flusher),
	}
}

func (c *FlushWriter) Write(b []byte) (int, error) {
	n, err := c.Writer.Write(b)
	c.Flusher.Flush()
	return n, err
}
