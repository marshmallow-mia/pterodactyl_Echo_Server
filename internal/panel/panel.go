// Package panel talks to Pterodactyl 1.x, Pterodactyl 2.x and Pelican through
// their application and client APIs. Operations a panel has no API for
// (Pterodactyl 1.x: egg import, mounts, API keys) live in local.go and need
// evr-ptero to run on the panel host.
package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Kind of panel.
type Kind string

const (
	Pterodactyl1 Kind = "pterodactyl1"
	Pterodactyl2 Kind = "pterodactyl2"
	Pelican      Kind = "pelican"
)

// Client is an API client for one panel.
type Client struct {
	URL       string // public panel URL, e.g. https://panel.example.com (also the websocket Origin)
	APIURL    string // optional address used for API calls instead of URL (e.g. http://127.0.0.1 behind a proxy)
	AppKey    string // application API key (ptla_… / peli_…)
	ClientKey string // account API key of an admin (ptlc_… / pacc_…), for power, console and Pterodactyl 2 admin calls
	Kind      Kind
	HTTP      *http.Client
}

// APIError is an error response.
type APIError struct {
	Status int
	Detail string
}

func (e *APIError) Error() string { return fmt.Sprintf("panel API: HTTP %d: %s", e.Status, e.Detail) }

// IsNotFound reports a 404.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *Client) do(ctx context.Context, method, path, key string, body io.Reader, contentType string, out any) error {
	base := c.URL
	if c.APIURL != "" {
		base = c.APIURL
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if c.Kind == Pterodactyl1 || c.Kind == "" {
		req.Header.Set("Accept", "Application/vnd.pterodactyl.v1+json")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Detail: errorDetail(data)}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func errorDetail(data []byte) string {
	var e struct {
		Errors []struct {
			Code, Detail string
			Meta         map[string]any
		} `json:"errors"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &e) == nil {
		var parts []string
		for _, x := range e.Errors {
			d := x.Detail
			if r, ok := x.Meta["source_field"].(string); ok {
				d = r + ": " + d
			}
			parts = append(parts, d)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
		if e.Message != "" {
			return e.Message
		}
	}
	s := strings.TrimSpace(string(data))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

func (c *Client) app(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	ct := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r, ct = bytes.NewReader(b), "application/json"
	}
	return c.do(ctx, method, "/api/application"+path, c.AppKey, r, ct, out)
}

func (c *Client) client(ctx context.Context, method, path string, body, out any) error {
	if c.ClientKey == "" {
		return errors.New("this needs an account (client) API key of a panel admin")
	}
	var r io.Reader
	ct := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r, ct = bytes.NewReader(b), "application/json"
	}
	return c.do(ctx, method, "/api/client"+path, c.ClientKey, r, ct, out)
}

// list collects every page of a list endpoint into out (a pointer to a slice
// of attribute structs).
func list[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var out []T
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for page := 1; ; page++ {
		var resp struct {
			Data []struct {
				Attributes T `json:"attributes"`
			} `json:"data"`
			Meta struct {
				Pagination struct {
					TotalPages int `json:"total_pages"`
				} `json:"pagination"`
			} `json:"meta"`
		}
		if err := c.app(ctx, http.MethodGet, fmt.Sprintf("%s%sper_page=100&page=%d", path, sep, page), nil, &resp); err != nil {
			return nil, err
		}
		for _, d := range resp.Data {
			out = append(out, d.Attributes)
		}
		if page >= resp.Meta.Pagination.TotalPages {
			return out, nil
		}
	}
}

// Detect works out the panel type from which endpoints exist.
func (c *Client) Detect(ctx context.Context) (Kind, error) {
	saved := c.Kind
	c.Kind = Pterodactyl1
	err := c.app(ctx, http.MethodGet, "/nests?per_page=1", nil, nil)
	if err == nil {
		return Pterodactyl1, nil
	}
	var ae *APIError
	if !errors.As(err, &ae) {
		c.Kind = saved
		return "", err
	}
	if ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden {
		c.Kind = saved
		return "", fmt.Errorf("the application API key was rejected: %w", err)
	}
	c.Kind = Pelican
	if err := c.app(ctx, http.MethodGet, "/mounts?per_page=1", nil, nil); err == nil {
		return Pelican, nil
	}
	c.Kind = Pterodactyl2
	if err := c.app(ctx, http.MethodGet, "/eggs?per_page=1", nil, nil); err == nil {
		return Pterodactyl2, nil
	}
	c.Kind = saved
	return "", errors.New("cannot tell which panel this is (not Pterodactyl 1.x/2.x or Pelican?)")
}

// Node is a Wings node.
type Node struct {
	ID           int    `json:"id"`
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	FQDN         string `json:"fqdn"`
	Scheme       string `json:"scheme"`
	DaemonListen int    `json:"daemon_listen"`
	LocationID   int    `json:"location_id"`
	Maintenance  bool   `json:"maintenance_mode"`
}

// Nodes lists nodes.
func (c *Client) Nodes(ctx context.Context) ([]Node, error) { return list[Node](ctx, c, "/nodes") }

// NodeSpec creates a node.
type NodeSpec struct {
	Name, FQDN, Scheme       string
	LocationID               int
	MemoryMB, DiskMB         int
	DaemonListen, DaemonSFTP int
	DaemonBase               string
	BehindProxy              bool
}

// CreateNode creates a node and returns it.
func (c *Client) CreateNode(ctx context.Context, s NodeSpec) (Node, error) {
	body := map[string]any{
		"name": s.Name, "fqdn": s.FQDN, "scheme": s.Scheme, "public": true, "behind_proxy": s.BehindProxy,
		"memory": s.MemoryMB, "memory_overallocate": 0, "disk": s.DiskMB, "disk_overallocate": 0,
		"upload_size": 100, "daemon_listen": s.DaemonListen, "daemon_sftp": s.DaemonSFTP, "daemon_base": s.DaemonBase,
	}
	if c.Kind == Pterodactyl1 {
		body["location_id"] = s.LocationID
	}
	if c.Kind == Pelican {
		body["cpu"], body["cpu_overallocate"] = 0, 0
	}
	var resp struct {
		Attributes Node `json:"attributes"`
	}
	err := c.app(ctx, http.MethodPost, "/nodes", body, &resp)
	return resp.Attributes, err
}

// NodeConfiguration returns the Wings config.yml content for a node (JSON).
func (c *Client) NodeConfiguration(ctx context.Context, id int) (map[string]any, error) {
	var cfg map[string]any
	err := c.app(ctx, http.MethodGet, fmt.Sprintf("/nodes/%d/configuration", id), nil, &cfg)
	return cfg, err
}

// Location (Pterodactyl 1.x).
type Location struct {
	ID    int    `json:"id"`
	Short string `json:"short"`
	Long  string `json:"long"`
}

// Locations lists locations (Pterodactyl 1.x only).
func (c *Client) Locations(ctx context.Context) ([]Location, error) {
	return list[Location](ctx, c, "/locations")
}

// CreateLocation creates a location (Pterodactyl 1.x only).
func (c *Client) CreateLocation(ctx context.Context, short, long string) (Location, error) {
	var resp struct {
		Attributes Location `json:"attributes"`
	}
	err := c.app(ctx, http.MethodPost, "/locations", map[string]string{"short": short, "long": long}, &resp)
	return resp.Attributes, err
}

// Allocation is an IP:port on a node.
type Allocation struct {
	ID       int    `json:"id"`
	IP       string `json:"ip"`
	Alias    string `json:"alias"`
	Port     int    `json:"port"`
	Assigned bool   `json:"assigned"`
}

// Allocations lists a node's allocations.
func (c *Client) Allocations(ctx context.Context, node int) ([]Allocation, error) {
	return list[Allocation](ctx, c, fmt.Sprintf("/nodes/%d/allocations", node))
}

// CreateAllocations adds ports (e.g. "6792" or "6792-6801") on ip.
func (c *Client) CreateAllocations(ctx context.Context, node int, ip, alias string, ports []string) error {
	body := map[string]any{"ip": ip, "ports": ports}
	if alias != "" {
		body["alias"] = alias
	}
	return c.app(ctx, http.MethodPost, fmt.Sprintf("/nodes/%d/allocations", node), body, nil)
}

// User is a panel user.
type User struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	RootAdmin bool   `json:"root_admin"`
}

// Users lists users.
func (c *Client) Users(ctx context.Context) ([]User, error) { return list[User](ctx, c, "/users") }

// Egg is an egg as the API lists it.
type Egg struct {
	ID           int               `json:"id"`
	UUID         string            `json:"uuid"`
	Name         string            `json:"name"`
	Nest         int               `json:"nest"`
	DockerImage  string            `json:"docker_image"`
	DockerImages map[string]string `json:"docker_images"`
	Startup      string            `json:"startup"`
}

// Nest groups eggs (Pterodactyl 1.x).
type Nest struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Eggs lists every egg.
func (c *Client) Eggs(ctx context.Context) ([]Egg, error) {
	if c.Kind != Pterodactyl1 {
		return list[Egg](ctx, c, "/eggs")
	}
	nests, err := list[Nest](ctx, c, "/nests")
	if err != nil {
		return nil, err
	}
	var out []Egg
	for _, n := range nests {
		eggs, err := list[Egg](ctx, c, fmt.Sprintf("/nests/%d/eggs", n.ID))
		if err != nil {
			return nil, err
		}
		for i := range eggs {
			eggs[i].Nest = n.ID
		}
		out = append(out, eggs...)
	}
	return out, nil
}

// FindEgg returns the egg with the given name.
func (c *Client) FindEgg(ctx context.Context, name string) (Egg, bool, error) {
	eggs, err := c.Eggs(ctx)
	if err != nil {
		return Egg{}, false, err
	}
	for _, e := range eggs {
		if e.Name == name {
			return e, true, nil
		}
	}
	return Egg{}, false, nil
}

// ImportEgg imports (or, with update set, re-imports) an egg through the API.
// Pelican takes it on the application API, Pterodactyl 2 on the admin API
// with a root admin's account key. Pterodactyl 1.x has no API for it: use
// Local.ImportEgg on the panel host.
func (c *Client) ImportEgg(ctx context.Context, data []byte, update *Egg) (Egg, error) {
	var resp struct {
		Attributes Egg `json:"attributes"`
	}
	switch c.Kind {
	case Pelican:
		path := "/eggs/import"
		if update != nil {
			path = fmt.Sprintf("/eggs/%d/import", update.ID)
		}
		err := c.do(ctx, http.MethodPost, "/api/application"+path, c.AppKey, bytes.NewReader(data), "application/yaml", &resp)
		return resp.Attributes, err
	case Pterodactyl2:
		if c.ClientKey == "" {
			return Egg{}, errors.New("importing eggs on Pterodactyl 2 needs a root admin's account API key")
		}
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("import_file", "egg-echovr-nevr.json")
		fw.Write(data)
		mw.Close()
		path := "/api/admin/eggs/import"
		if update != nil {
			path = fmt.Sprintf("/api/admin/eggs/%d/import", update.ID)
		}
		err := c.do(ctx, http.MethodPost, path, c.ClientKey, &body, mw.FormDataContentType(), &resp)
		return resp.Attributes, err
	}
	return Egg{}, errors.New("Pterodactyl 1.x cannot import eggs over its API; run evr-ptero on the panel host or import egg/egg-echovr-nevr.json in Admin → Nests")
}

// Server is a panel server.
type Server struct {
	ID          int    `json:"id"`
	UUID        string `json:"uuid"`
	Identifier  string `json:"identifier"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Suspended   bool   `json:"suspended"`
	Status      any    `json:"status"`
	User        int    `json:"user"`
	Node        int    `json:"node"`
	Allocation  int    `json:"allocation"`
	Egg         int    `json:"egg"`
	Nest        int    `json:"nest"`
	Container   struct {
		StartupCommand string            `json:"startup_command"`
		Image          string            `json:"image"`
		Environment    map[string]string `json:"-"`
		RawEnv         map[string]any    `json:"environment"`
	} `json:"container"`
	Limits struct {
		Memory int `json:"memory"`
		Disk   int `json:"disk"`
		CPU    int `json:"cpu"`
	} `json:"limits"`
}

// Env returns the server's environment as strings.
func (s Server) Env() map[string]string {
	out := map[string]string{}
	for k, v := range s.Container.RawEnv {
		switch t := v.(type) {
		case string:
			out[k] = t
		case float64:
			out[k] = strconv.FormatFloat(t, 'f', -1, 64)
		case bool:
			out[k] = strconv.FormatBool(t)
		case nil:
			out[k] = ""
		default:
			b, _ := json.Marshal(t)
			out[k] = string(b)
		}
	}
	return out
}

// Servers lists servers.
func (c *Client) Servers(ctx context.Context) ([]Server, error) {
	return list[Server](ctx, c, "/servers")
}

// ServerSpec creates a server.
type ServerSpec struct {
	Name, Description string
	User, Egg         int
	Image, Startup    string
	Env               map[string]string
	Allocation        int
	MemoryMB, DiskMB  int
	CPU               int // percent, 0 = unlimited
	StartAfterInstall bool
}

// CreateServer creates a server.
func (c *Client) CreateServer(ctx context.Context, s ServerSpec) (Server, error) {
	body := map[string]any{
		"name": s.Name, "description": s.Description, "user": s.User, "egg": s.Egg,
		"docker_image": s.Image, "startup": s.Startup, "environment": s.Env,
		"limits":              map[string]int{"memory": s.MemoryMB, "swap": 0, "disk": s.DiskMB, "io": 500, "cpu": s.CPU},
		"feature_limits":      map[string]int{"databases": 0, "allocations": 0, "backups": 0},
		"allocation":          map[string]any{"default": s.Allocation},
		"start_on_completion": s.StartAfterInstall,
	}
	var resp struct {
		Attributes Server `json:"attributes"`
	}
	err := c.app(ctx, http.MethodPost, "/servers", body, &resp)
	return resp.Attributes, err
}

// UpdateStartup changes a server's egg, image, startup and environment.
func (c *Client) UpdateStartup(ctx context.Context, s Server, egg int, image, startup string, env map[string]string, skipScripts bool) error {
	body := map[string]any{"startup": startup, "environment": env, "egg": egg, "image": image, "skip_scripts": skipScripts}
	return c.app(ctx, http.MethodPatch, fmt.Sprintf("/servers/%d/startup", s.ID), body, nil)
}

// DeleteServer deletes a server.
func (c *Client) DeleteServer(ctx context.Context, id int, force bool) error {
	path := fmt.Sprintf("/servers/%d", id)
	if force {
		path += "/force"
	}
	return c.app(ctx, http.MethodDelete, path, nil, nil)
}

// Reinstall runs the egg's install script again.
func (c *Client) Reinstall(ctx context.Context, id int) error {
	return c.app(ctx, http.MethodPost, fmt.Sprintf("/servers/%d/reinstall", id), nil, nil)
}

// Power sends start, stop, restart or kill (client API).
func (c *Client) Power(ctx context.Context, identifier, signal string) error {
	return c.client(ctx, http.MethodPost, "/servers/"+identifier+"/power", map[string]string{"signal": signal}, nil)
}

// Command writes a console command (client API).
func (c *Client) Command(ctx context.Context, identifier, command string) error {
	return c.client(ctx, http.MethodPost, "/servers/"+identifier+"/command", map[string]string{"command": command}, nil)
}

// Resources is the live state of a server (client API).
type Resources struct {
	State string `json:"current_state"`
	Usage struct {
		Memory int64   `json:"memory_bytes"`
		CPU    float64 `json:"cpu_absolute"`
		Uptime int64   `json:"uptime"`
	} `json:"resources"`
}

// Resources returns a server's power state and usage.
func (c *Client) Resources(ctx context.Context, identifier string) (Resources, error) {
	var resp struct {
		Attributes Resources `json:"attributes"`
	}
	err := c.client(ctx, http.MethodGet, "/servers/"+identifier+"/resources", nil, &resp)
	return resp.Attributes, err
}

// Mount is a panel mount.
type Mount struct {
	ID       int    `json:"id"`
	UUID     string `json:"uuid"`
	Name     string `json:"name"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

// EnsureMount creates (or finds by name) a read-only mount and attaches it to
// the eggs and nodes, on panels that have an API for it.
func (c *Client) EnsureMount(ctx context.Context, name, source, target string, eggs, nodes []int) (Mount, error) {
	var base, key string
	switch c.Kind {
	case Pelican:
		base, key = "/api/application/mounts", c.AppKey
	case Pterodactyl2:
		base, key = "/api/admin/mounts", c.ClientKey
	default:
		return Mount{}, errors.New("Pterodactyl 1.x has no mount API; run evr-ptero on the panel host or add the mount in Admin → Mounts")
	}
	var listed struct {
		Data []struct {
			Attributes Mount `json:"attributes"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, base+"?per_page=100", key, nil, "", &listed); err != nil {
		return Mount{}, err
	}
	var m Mount
	for _, d := range listed.Data {
		if d.Attributes.Name == name {
			m = d.Attributes
		}
	}
	if m.ID == 0 {
		b, _ := json.Marshal(map[string]any{"name": name, "source": source, "target": target, "read_only": true, "user_mountable": false, "description": "Echo VR shared files (evr-ptero)"})
		var resp struct {
			Attributes Mount `json:"attributes"`
		}
		if err := c.do(ctx, http.MethodPost, base, key, bytes.NewReader(b), "application/json", &resp); err != nil {
			return Mount{}, err
		}
		m = resp.Attributes
	}
	for kind, ids := range map[string][]int{"eggs": eggs, "nodes": nodes} {
		if len(ids) == 0 {
			continue
		}
		b, _ := json.Marshal(map[string][]int{kind: ids})
		if err := c.do(ctx, http.MethodPost, fmt.Sprintf("%s/%d/%s", base, m.ID, kind), key, bytes.NewReader(b), "application/json", nil); err != nil {
			return m, fmt.Errorf("attaching mount %s to %s: %w", name, kind, err)
		}
	}
	return m, nil
}

// Escape is url.PathEscape, exported for callers building client paths.
func Escape(s string) string { return url.PathEscape(s) }

// AttachServerMounts attaches mounts to a server on panels with a mount API.
func (c *Client) AttachServerMounts(ctx context.Context, serverID int, mounts []int) error {
	switch c.Kind {
	case Pelican:
		for _, m := range mounts {
			b, _ := json.Marshal(map[string][]int{"servers": {serverID}})
			if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/application/mounts/%d/servers", m), c.AppKey, bytes.NewReader(b), "application/json", nil); err != nil {
				return err
			}
		}
		return nil
	case Pterodactyl2:
		for _, m := range mounts {
			b, _ := json.Marshal(map[string]int{"mount_id": m})
			if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/admin/servers/%d/mounts", serverID), c.ClientKey, bytes.NewReader(b), "application/json", nil); err != nil {
				return err
			}
		}
		return nil
	}
	return errors.New("Pterodactyl 1.x cannot attach mounts over its API; run evr-ptero on the panel host or attach the mounts in Admin → Servers → Mounts")
}
