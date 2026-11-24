package servers

import (
	"testing"
	"time"
)

const sampleProbe = `@@os
Ubuntu 24.04.1 LTS
@@kernel
6.8.0-45-generic
@@arch
x86_64
@@hostname
web-01
@@nproc
4
@@loadavg
0.52 0.40 0.31 1/234 5678
@@uptime
86400.55 300000.00
@@meminfo
MemTotal:        8000000 kB
MemAvailable:    6000000 kB
SwapTotal:       2000000 kB
SwapFree:        2000000 kB
@@disk
/dev/sda1  100000000 40000000 55000000  43% /
@@cpu1
cpu  1000 0 500 8000 500 0 0 0 0 0
@@cpu2
cpu  1100 0 550 8300 550 0 0 0 0 0
@@docker
running 27.3.1
`

func TestParseProbe(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	info, m, err := parseProbe(sampleProbe, now)
	if err != nil {
		t.Fatal(err)
	}

	wantInfo := SystemInfo{
		OS: "Ubuntu 24.04.1 LTS", Kernel: "6.8.0-45-generic", Arch: "x86_64", Hostname: "web-01",
		CPUCores: 4, DockerStatus: DockerRunning, DockerVersion: "27.3.1",
	}
	if info != wantInfo {
		t.Errorf("info = %+v\nwant %+v", info, wantInfo)
	}

	// delta total = 500, delta idle (idle+iowait) = 350 → 30% de uso
	if m.CPUPercent != 30 {
		t.Errorf("cpu = %v, want 30", m.CPUPercent)
	}
	if m.Load != [3]float64{0.52, 0.40, 0.31} {
		t.Errorf("load = %v", m.Load)
	}
	if m.Memory != (Usage{Total: 8000000 * 1024, Used: 2000000 * 1024}) {
		t.Errorf("memory = %+v", m.Memory)
	}
	if m.Swap.Used != 0 || m.Swap.Total != 2000000*1024 {
		t.Errorf("swap = %+v", m.Swap)
	}
	if m.Disk != (Usage{Total: 95000000 * 1024, Used: 40000000 * 1024}) {
		t.Errorf("disk = %+v", m.Disk)
	}
	if m.UptimeSeconds != 86400 {
		t.Errorf("uptime = %v", m.UptimeSeconds)
	}
}

func TestParseProbeDockerStates(t *testing.T) {
	base := "@@meminfo\nMemTotal: 1000 kB\nMemAvailable: 500 kB\n@@docker\n"
	for out, want := range map[string]DockerStatus{
		"not_installed": DockerNotInstalled,
		"no_permission": DockerNoPermission,
		"not_running":   DockerNotRunning,
	} {
		info, _, err := parseProbe(base+out+"\n", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if info.DockerStatus != want || info.DockerVersion != "" {
			t.Errorf("%s: got %q %q", out, info.DockerStatus, info.DockerVersion)
		}
	}
}

func TestParseProbeRejectsNonLinux(t *testing.T) {
	if _, _, err := parseProbe("@@os\n@@meminfo\n", time.Now()); err == nil {
		t.Fatal("esperaba error sin /proc/meminfo")
	}
}

func TestNormalizeInput(t *testing.T) {
	ok := CreateInput{Name: " web ", Host: "10.0.0.5", Username: "root", AuthMethod: AuthInstanceKey}
	if err := ok.normalize(); err != nil {
		t.Fatal(err)
	}
	if ok.Port != 22 || ok.Name != "web" {
		t.Errorf("normalize: %+v", ok)
	}

	bad := []CreateInput{
		{Host: "h", Username: "u", AuthMethod: AuthInstanceKey},
		{Name: "n", Host: "root@h", Username: "u", AuthMethod: AuthInstanceKey},
		{Name: "n", Host: "h", Username: "u", AuthMethod: AuthPassword},
		{Name: "n", Host: "h", Username: "u", AuthMethod: "magic"},
		{Name: "n", Host: "h", Port: 70000, Username: "u", AuthMethod: AuthInstanceKey},
	}
	for _, in := range bad {
		if err := in.normalize(); err == nil {
			t.Errorf("esperaba error para %+v", in)
		}
	}
}
