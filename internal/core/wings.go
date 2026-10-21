package core

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnsureAllowedMounts adds paths to the local Wings config's allowed_mounts
// and restarts Wings when something changed. Wings ignores allowed_mounts in
// configuration pushed by the panel, so it has to be edited on the node.
// Restarting Wings does not stop running servers.
func EnsureAllowedMounts(paths []string) (changed bool, file string, err error) {
	_, file, err = ReadWingsConfig()
	if err != nil {
		return false, "", fmt.Errorf("no Wings config on this host: %w", err)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return false, file, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return false, file, fmt.Errorf("%s: unexpected format", file)
	}
	root := doc.Content[0]
	var list *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "allowed_mounts" {
			list = root.Content[i+1]
		}
	}
	if list == nil {
		list = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "allowed_mounts"}, list)
	}
	if list.Kind != yaml.SequenceNode {
		list.Kind, list.Tag, list.Value, list.Content = yaml.SequenceNode, "!!seq", "", nil
	}
	have := map[string]bool{}
	for _, n := range list.Content {
		have[strings.TrimRight(n.Value, "/")] = true
	}
	for _, p := range paths {
		if !have[strings.TrimRight(p, "/")] {
			list.Content = append(list.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p})
			changed = true
		}
	}
	if !changed {
		return false, file, nil
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return false, file, err
	}
	fi, _ := os.Stat(file)
	mode := os.FileMode(0o600)
	if fi != nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(file+".evr-bak", b, mode); err != nil {
		return false, file, err
	}
	if err := os.WriteFile(file, out, mode); err != nil {
		return false, file, err
	}
	for _, unit := range []string{"wings", "pelican-wings"} {
		if exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil {
			if out, err := exec.Command("systemctl", "restart", unit).CombinedOutput(); err != nil {
				return true, file, fmt.Errorf("restarting %s: %v: %s", unit, err, out)
			}
		}
	}
	return true, file, nil
}
