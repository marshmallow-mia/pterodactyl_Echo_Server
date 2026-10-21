package egg

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Laravel splits a rule string on "|", so no single rule may contain one.
func TestRulesHaveNoPipes(t *testing.T) {
	for _, v := range Variables() {
		for _, r := range v.Rules {
			if strings.Contains(r, "|") {
				t.Errorf("%s: rule %q contains |", v.Env, r)
			}
			if re, ok := strings.CutPrefix(r, "regex:"); ok {
				body := strings.TrimSuffix(strings.TrimPrefix(re, "/"), "/")
				rx, err := regexp.Compile(body)
				if err != nil {
					t.Errorf("%s: %v", v.Env, err)
					continue
				}
				if v.Default != "" && !rx.MatchString(v.Default) {
					t.Errorf("%s: default %q does not match its own rule", v.Env, v.Default)
				}
			}
		}
	}
	rx := regexp.MustCompile(`^(current)?(v[0-9]+\.[0-9]+\.[0-9]+[0-9A-Za-z.-]*)?$`)
	for _, ok := range []string{"current", "v5.4.2", "v6.0.0-rc.1"} {
		if !rx.MatchString(ok) {
			t.Errorf("%s rejected", ok)
		}
	}
	if rx.MatchString("latest") {
		t.Error("latest accepted")
	}
}

func TestRenderFormats(t *testing.T) {
	s := Spec{Image: "ghcr.io/x/echovr-pterodactyl:2", Version: "test"}
	b, err := PTDL(s)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p["meta"].(map[string]any)["version"] != "PTDL_v2" || len(p["variables"].([]any)) != len(Variables()) {
		t.Errorf("ptdl: %v", p["meta"])
	}
	var st map[string]any
	json.Unmarshal([]byte(p["config"].(map[string]any)["startup"].(string)), &st)
	if st["done"] != ReadyMarker {
		t.Errorf("done string %v", st["done"])
	}
	y, err := PLCN(s)
	if err != nil {
		t.Fatal(err)
	}
	var e map[string]any
	if err := yaml.Unmarshal(y, &e); err != nil {
		t.Fatal(err)
	}
	if e["meta"].(map[string]any)["version"] != "PLCN_v3" || e["uuid"] != UUID {
		t.Errorf("plcn meta %v", e["meta"])
	}
}
