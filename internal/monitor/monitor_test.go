package monitor

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const statA = "cpu  1000 0 500 8000 100 0 0 0 0 0\ncpu0 500 0 250 4000 50 0 0 0 0 0\ncpu1 500 0 250 4000 50 0 0 0 0 0\nintr 1\n"
const statB = "cpu  1100 0 550 8300 150 0 0 0 0 0\ncpu0 550 0 275 4150 75 0 0 0 0 0\ncpu1 550 0 275 4150 75 0 0 0 0 0\n"
const meminfoText = "MemTotal:        1000000 kB\nMemFree:          100000 kB\nMemAvailable:     400000 kB\nBuffers: 1 kB\nCached: 2 kB\nSwapTotal:       1048576 kB\nSwapFree:         524288 kB\n"
const netA = "Inter-|   Receive\n face |bytes\n  lo: 999999 1 0 0 0 0 0 0 999999 1 0 0 0 0 0 0\nens3: 1000 5 0 0 0 0 0 0 2000 5 0 0 0 0 0 0\nveth1: 5000 5 0 0 0 0 0 0 5000 5 0 0 0 0 0 0\n"
const netB = "Inter-|   Receive\n face |bytes\n  lo: 1999999 1 0 0 0 0 0 0 1999999 1 0 0 0 0 0 0\nens3: 7000 5 0 0 0 0 0 0 5000 5 0 0 0 0 0 0\nveth1: 9000 5 0 0 0 0 0 0 9000 5 0 0 0 0 0 0\n"

func TestParsers(t *testing.T) {
	c, cores, err := parseStat([]byte(statA))
	if err != nil || cores != 2 || c.total != 9600 || c.busy != 1500 {
		t.Errorf("stat: %+v cores=%d err=%v (idle and iowait are not busy)", c, cores, err)
	}
	if _, _, err := parseStat([]byte("intr 1\n")); err == nil {
		t.Error("no cpu line must be an error")
	}
	if _, _, err := parseStat([]byte("cpu 1 2 3\n")); err == nil {
		t.Error("a short cpu line must be an error")
	}
	m, err := parseMeminfo([]byte(meminfoText))
	if err != nil || math.Abs(m.total-976.5625) > 0.01 || math.Abs(m.used-585.9375) > 0.01 || m.swapTotal != 1024 || m.swapUsed != 512 {
		t.Errorf("meminfo: %+v %v (used is total minus available)", m, err)
	}
	if m, err = parseMeminfo([]byte("MemTotal: 2048 kB\nMemFree: 1024 kB\nBuffers: 256 kB\nCached: 256 kB\n")); err != nil || m.used != 0.5 {
		t.Errorf("old kernel without MemAvailable: %+v %v", m, err)
	}
	if _, err := parseMeminfo([]byte("nothing")); err == nil {
		t.Error("no MemTotal must be an error")
	}
	if l, err := parseLoad([]byte("0.52 0.40 0.30 1/200 999\n")); err != nil || l != 0.52 {
		t.Errorf("load %v %v", l, err)
	}
	if u, err := parseUptime([]byte("3600.25 100.0\n")); err != nil || u != 3600.25 {
		t.Errorf("uptime %v %v", u, err)
	}
	n, _ := parseNetDev([]byte(netA))
	if n.rx != 1000 || n.tx != 2000 {
		t.Errorf("net: %+v (loopback and virtual interfaces do not count)", n)
	}
}

func TestMountPointsKeepOneRealDevicePerEntryAndSkipTheRest(t *testing.T) {
	got := mountPoints([]byte("/dev/vda1 / ext4 rw 0 0\n/dev/vda1 /var/lib/docker/x ext4 rw 0 0\ntmpfs /run tmpfs rw 0 0\n/dev/loop1 /snap/core squashfs ro 0 0\n/dev/vdb /home ext4 rw 0 0\nproc /proc proc rw 0 0\n/dev/vdc /mnt/my\\040disk xfs rw 0 0\n"))
	want := []string{"/", "/home", "/mnt/my disk"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%v want %v", got, want)
		}
	}
}

type fakeProc struct {
	files map[string]string
}

func (f *fakeProc) read(name string) ([]byte, error) {
	if s, ok := f.files[name]; ok {
		return []byte(s), nil
	}
	return nil, errors.New("no such file")
}

func TestTheFirstTickOnlyRecordsAndTheSecondGivesRates(t *testing.T) {
	p := &fakeProc{files: map[string]string{"stat": statA, "meminfo": meminfoText, "loadavg": "0.5 0 0 1/1 1", "net/dev": netA}}
	c := New(p.read, "")
	at := time.Unix(1_000_000, 0)
	c.now = func() time.Time { return at }
	if err := c.Tick(); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Latest(); ok {
		t.Error("a rate needs two readings")
	}
	p.files["stat"], p.files["net/dev"] = statB, netB
	at = at.Add(30 * time.Second)
	if err := c.Tick(); err != nil {
		t.Fatal(err)
	}
	s, ok := c.Latest()
	if !ok {
		t.Fatal("no sample")
	}
	// busy went 1500 -> 1650 over 9600 -> 10100 total ticks: 150 / 500
	if math.Abs(s.CPU-30) > 0.01 || math.Abs(s.Rx-200) > 0.01 || math.Abs(s.Tx-100) > 0.01 || s.Load != 0.5 || math.Abs(s.MemMB-585.9375) > 0.01 || s.T != at.Unix() {
		t.Errorf("%+v", s)
	}
	if m := c.Machine(); m.Cores != 2 || m.SwapTotalMB != 1024 {
		t.Errorf("%+v", m)
	}
}

func TestACounterThatGoesBackwardsGivesZeroNotANegativeRate(t *testing.T) {
	p := &fakeProc{files: map[string]string{"stat": statB, "meminfo": meminfoText, "net/dev": netB}}
	c := New(p.read, "")
	at := time.Unix(1_000_000, 0)
	c.now = func() time.Time { return at }
	_ = c.Tick()
	p.files["stat"], p.files["net/dev"] = statA, netA // a reboot, or a driver that was reloaded
	at = at.Add(30 * time.Second)
	if err := c.Tick(); err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Latest(); s.CPU != 0 || s.Rx != 0 || s.Tx != 0 {
		t.Errorf("%+v", s)
	}
}

func TestAnUnreadableSourceIsAnErrorAndAddsNothing(t *testing.T) {
	c := New((&fakeProc{files: map[string]string{}}).read, "")
	if err := c.Tick(); err == nil {
		t.Error("an error is expected")
	}
	if _, ok := c.Latest(); ok {
		t.Error("nothing must be added")
	}
}

func TestOnlyTheLastDayIsKeptAndSinceSelects(t *testing.T) {
	c := New((&fakeProc{}).read, "")
	base := int64(2_000_000)
	hour := int64(time.Hour / time.Second)
	for i, off := range []int64{0, 12 * hour, 25 * hour} {
		c.Add(Sample{T: base + off, CPU: float64(i)})
	}
	got := c.Since(time.Unix(0, 0))
	if len(got) != 2 || got[0].CPU != 1 {
		t.Errorf("a sample more than a day older than the newest must go: %+v", got)
	}
	if got := c.Since(time.Unix(base+12*hour+1, 0)); len(got) != 1 || got[0].CPU != 2 {
		t.Errorf("%+v", got)
	}
}

func TestDownsampleAveragesBucketsAndKeepsGaps(t *testing.T) {
	var s []Sample
	for i := int64(0); i < 100; i++ {
		if i >= 40 && i < 60 { // a gap: the panel was not running
			continue
		}
		s = append(s, Sample{T: 1000 + i*10, CPU: float64(i % 10), Rx: 100})
	}
	from, to := time.Unix(1000, 0), time.Unix(2000, 0)
	got := Downsample(s, from, to, 10)
	if len(got) != 8 {
		t.Fatalf("ten buckets, two of them empty: %d", len(got))
	}
	for _, x := range got {
		if math.Abs(x.CPU-4.5) > 0.001 || x.Rx != 100 {
			t.Errorf("a bucket of ten consecutive values 0..9 averages 4.5: %+v", x)
		}
	}
	if same := Downsample(s[:5], from, to, 10); len(same) != 5 {
		t.Error("fewer samples than points are returned as they are")
	}
	if Downsample(nil, from, to, 10) != nil || Downsample(s, to, from, 10) != nil || Downsample(s, from, to, 0) != nil {
		t.Error("nothing to draw")
	}
}

func TestSamplesSurviveARestartAndOldOnesDoNot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "metrics.json")
	now := time.Unix(5_000_000, 0)
	at := func(d time.Duration) func() time.Time { return func() time.Time { return now.Add(d) } }
	c := newWithClock((&fakeProc{}).read, file, at(0))
	c.Add(Sample{T: now.Unix() - 100, CPU: 7})
	c.save()
	st, err := os.Stat(file)
	if err != nil {
		t.Fatalf("the file must exist: %v", err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("the file must be private: %v", st.Mode())
	}
	if got := newWithClock((&fakeProc{}).read, file, at(10)).Since(time.Unix(0, 0)); len(got) != 1 || got[0].CPU != 7 {
		t.Errorf("after a restart: %+v", got)
	}
	// two days later the same file gives nothing
	if got := newWithClock((&fakeProc{}).read, file, at(48*time.Hour)).Since(time.Unix(0, 0)); len(got) != 0 {
		t.Errorf("old samples must not come back: %+v", got)
	}
	// a damaged file starts empty and does not stop the panel
	_ = os.WriteFile(file, []byte("{not json"), 0o600)
	if got := newWithClock((&fakeProc{}).read, file, at(0)).Since(time.Unix(0, 0)); len(got) != 0 {
		t.Error("damaged file")
	}
}
