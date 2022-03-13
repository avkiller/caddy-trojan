package trojan

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/caddyserver/caddy/v2"
)

func init() {
	caddy.RegisterModule(Admin{})
}

// Admin is ...
type Admin struct {
	// Upstream is ...
	Upstream Upstream
}

// CaddyModule returns the Caddy module information.
func (Admin) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "admin.api.trojan",
		New: func() caddy.Module { return new(Admin) },
	}
}

// Provision is ...
func (al *Admin) Provision(ctx caddy.Context) error {
	al.Upstream = NewUpstream(ctx.Storage())
	return nil
}

// Routes returns a route for the /trojan/* endpoint.
func (al *Admin) Routes() []caddy.AdminRoute {
	return []caddy.AdminRoute{
		{
			Pattern: "/trojan/users",
			Handler: caddy.AdminHandlerFunc(al.GetUsers),
		},
		{
			Pattern: "/trojan/users/add",
			Handler: caddy.AdminHandlerFunc(al.AddUser),
		},
		{
			Pattern: "/trojan/users/del",
			Handler: caddy.AdminHandlerFunc(al.DelUser),
		},
	}
}

// GetUsers is ...
func (al *Admin) GetUsers(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("get trojan user method error")
	}

	type User struct {
		Key  string `json:"key"`
		Up   int64  `json:"up"`
		Down int64  `json:"down"`
	}

	users := make([]User, 0)
	al.Upstream.Range(func(key string, up, down int64) {
		users = append(users, User{Key: key, Up: up, Down: down})
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(users)
	return nil
}

// AddUser is ...
func (al *Admin) AddUser(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("add trojan user method error")
	}

	type User struct {
		Password string `json:"password,omitempty"`
		Key      string `json:"key,omitempty"`
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	user := User{}
	if err := json.Unmarshal(b, &user); err != nil {
		return err
	}
	if user.Key != "" {
		al.Upstream.AddKey(user.Key)

		w.WriteHeader(http.StatusOK)
		return nil
	}
	if user.Password != "" {
		al.Upstream.Add(user.Password)
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

// DelUser is ...
func (al *Admin) DelUser(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodDelete {
		return errors.New("delete trojan user method error")
	}

	type User struct {
		Password string `json:"password,omitempty"`
		Key      string `json:"key,omitempty"`
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	user := User{}
	if err := json.Unmarshal(b, &user); err != nil {
		return err
	}
	if user.Key != "" {
		al.Upstream.DelKey(user.Key)

		w.WriteHeader(http.StatusOK)
		return nil
	}
	if user.Password != "" {
		al.Upstream.Del(user.Password)
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

// Interface guards
var _ caddy.Provisioner = (*Handler)(nil)
