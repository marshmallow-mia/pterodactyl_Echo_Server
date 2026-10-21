package panelinstall

import (
	"testing"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/ghrel"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr"
)

func rel(tag string, pre bool) ghrel.Release {
	return ghrel.Release{Tag: tag, Version: nevr.MustParseVersion(tag), Prerelease: pre}
}

func TestPanelChannels(t *testing.T) {
	stable := Installed{Product: "pterodactyl", Channel: "stable", Panel: "1.15.1"}
	beta := Installed{Product: "pterodactyl", Channel: "beta", Panel: "2.0.0-beta.2"}
	pelican := Installed{Product: "pelican", Channel: "stable", Panel: "1.0.0-beta38"}
	for _, c := range []struct {
		in      Installed
		r       ghrel.Release
		channel string
		want    bool
	}{
		{stable, rel("v1.15.2", false), "", true},
		{stable, rel("v2.0.0-beta.3", true), "", false}, // 1.x users stay on 1.x
		{stable, rel("v2.0.0-beta.3", true), "beta", true},
		{stable, rel("v2.0.0", false), "", false}, // a new major is a channel switch
		{beta, rel("v2.0.0-beta.3", true), "", true},
		{beta, rel("v2.0.0", false), "", true},
		{beta, rel("v1.15.2", false), "", false},
		{pelican, rel("v1.0.0-rc.1", false), "", true},
	} {
		if got := c.in.InChannel(c.r, c.channel); got != c.want {
			t.Errorf("%s %s on %q: %v", c.in.Panel, c.r.Tag, c.channel, got)
		}
	}
}
