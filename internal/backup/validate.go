package backup

import (
	"fmt"
	"strings"
)

// EntryError reports one validation failure. Index is -1 for archive-level
// problems such as an unsupported version.
type EntryError struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	Field string `json:"field"`
	Msg   string `json:"message"`
}

func (e EntryError) Error() string {
	if e.Index < 0 {
		return fmt.Sprintf("%s: %s", e.Field, e.Msg)
	}
	return fmt.Sprintf("tunnels[%d] (%s): %s: %s", e.Index, e.Name, e.Field, e.Msg)
}

var (
	validTypes       = map[string]bool{"local": true, "remote": true, "dynamic": true}
	validAuthMethods = map[string]bool{"key": true, "password": true, "agent": true, "cert": true}
	validDesired     = map[string]bool{"": true, "active": true, "stopped": true}
)

// ValidateArchive checks every entry and returns all problems at once, so a
// broken file can be fixed in a single pass. It returns nil when valid.
//
// Validation is deliberately more permissive than api.CreateTunnelRequest:
// remote_host and remote_port are only required for local and remote tunnels,
// so any archive this package exports can always be re-imported.
func ValidateArchive(a *Archive) []EntryError {
	if a.Version != SchemaVersion {
		return []EntryError{{
			Index: -1,
			Field: "version",
			Msg:   fmt.Sprintf("unsupported archive version %d, expected %d", a.Version, SchemaVersion),
		}}
	}

	var errs []EntryError
	seen := make(map[string]int, len(a.Tunnels))

	for i, e := range a.Tunnels {
		add := func(field, msg string) {
			errs = append(errs, EntryError{Index: i, Name: e.Name, Field: field, Msg: msg})
		}

		name := strings.TrimSpace(e.Name)
		switch {
		case name == "":
			add("name", "must not be empty")
		case len(name) > 100:
			add("name", "must be 100 characters or fewer")
		default:
			if prev, dup := seen[name]; dup {
				add("name", fmt.Sprintf("duplicate name, already used by tunnels[%d]", prev))
			} else {
				seen[name] = i
			}
		}

		if !validTypes[e.Type] {
			add("type", fmt.Sprintf("must be local, remote or dynamic (got %q)", e.Type))
		}

		if !validDesired[e.DesiredStatus] {
			add("desired_status", fmt.Sprintf("must be active or stopped (got %q)", e.DesiredStatus))
		}

		if e.LocalPort < 0 || e.LocalPort > 65535 {
			add("local_port", "must be between 0 and 65535")
		}

		// A dynamic tunnel is a SOCKS5 proxy with no fixed destination.
		if e.Type != "dynamic" {
			if strings.TrimSpace(e.RemoteHost) == "" {
				add("remote_host", "must not be empty")
			}
			if e.RemotePort < 1 || e.RemotePort > 65535 {
				add("remote_port", "must be between 1 and 65535")
			}
		}

		if len(e.Hops) == 0 {
			add("hops", "at least one hop is required")
		}
		for j, h := range e.Hops {
			if strings.TrimSpace(h.Host) == "" {
				add(fmt.Sprintf("hops[%d].host", j), "must not be empty")
			}
			if strings.TrimSpace(h.User) == "" {
				add(fmt.Sprintf("hops[%d].user", j), "must not be empty")
			}
			if h.Port < 1 || h.Port > 65535 {
				add(fmt.Sprintf("hops[%d].port", j), "must be between 1 and 65535")
			}
			if h.AuthMethod != "" && !validAuthMethods[h.AuthMethod] {
				add(fmt.Sprintf("hops[%d].auth_method", j),
					fmt.Sprintf("must be key, password, agent or cert (got %q)", h.AuthMethod))
			}
		}
	}

	return errs
}
