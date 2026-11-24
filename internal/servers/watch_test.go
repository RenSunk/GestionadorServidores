package servers

import "testing"

func TestParseContainers(t *testing.T) {
	out := "gs-web|running\ngs-api|exited\nother|running\ngs-web-next|running\n@@stats\ngs-web|12.5%|30.25%\ngs-web-next|1%|2%\n"
	got := parseContainers(out)
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if got[0] != (Container{Name: "gs-web", State: "running", CPU: 12.5, Mem: 30.25, Sampled: true}) {
		t.Errorf("web: %+v", got[0])
	}
	if got[1].Name != "gs-api" || got[1].State != "exited" || got[1].Sampled {
		t.Errorf("api: %+v", got[1])
	}
	if len(parseContainers("@@stats\n")) != 0 {
		t.Error("sin contenedores")
	}
}
