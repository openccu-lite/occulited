package httpapi

// occulited task 159: System → Storage's "Disk writes" - what was written to each disk since the
// boot and in the last hour and day (always known, /proc/diskstats), and by which service (the
// kernel's io controller, counted only while the Services page's switch "record disk writes per
// service" is on). The figures are internal/iowatch's, which keeps them in memory; the switch is
// system.IOAccounting, a systemd manager drop-in in /run.

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/hobbyquaker/occulited/internal/auth"
	"github.com/hobbyquaker/occulited/internal/iowatch"
)

// WriteHintBytesPerHour is the write rate of an SD card or eMMC above which the Storage page
// hints at it: 16 MiB an hour, 140 GiB a year. An idle system writes a few MiB an hour (the
// journal's copies, the databases of the interface processes); several times that is a service
// that writes more than it needs to, on a medium that wears with every write.
const WriteHintBytesPerHour = 16 << 20

// writeHintMinSeconds is the history a hint needs: a burst right after a start is none.
const writeHintMinSeconds = 30 * 60

func (a *SystemAPI) registerStorageWrites(mux *http.ServeMux, p string) {
	route(mux, auth.ScopeSystemRead, "GET "+p+"/storage/writes", a.storageWrites)
	route(mux, auth.ScopeSystemRead, "GET "+p+"/storage/writes/recording", a.storageWritesRecording)
	route(mux, auth.ScopeSystemWrite, "PUT "+p+"/storage/writes/recording", a.storageWritesRecordingPut)
}

// writesRecording is the switch and what came of it.
type writesRecording struct {
	// Supported: the kernel (in a container: the host) offers the io controller and the system
	// runs systemd. Enabled: the switch. Active: the services are being counted.
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
	Active    bool `json:"active"`
	// Since is when the recording was first seen active; absent while it is not.
	Since *time.Time `json:"since,omitempty"`
}

type writesPartition struct {
	iowatch.Partition
	// Mount and Role (root, boot, userfs, other) of a mounted partition
	Mount string `json:"mount,omitempty"`
	Role  string `json:"role,omitempty"`
}

type writesDisk struct {
	Name string `json:"name"`
	// Kind, Vendor and Model as the Storage health panel has them; empty for a disk it does not list
	Kind         string            `json:"kind,omitempty"`
	Vendor       string            `json:"vendor,omitempty"`
	Model        string            `json:"model,omitempty"`
	ReadBytes    uint64            `json:"read_bytes"`
	WrittenBytes uint64            `json:"written_bytes"`
	Hour         iowatch.Window    `json:"hour"`
	Day          iowatch.Window    `json:"day"`
	Partitions   []writesPartition `json:"partitions"`
	// Hint: the disk is an SD card or eMMC and its rate over the last day (the history there is,
	// at least half an hour) is above WriteHintBytesPerHour; the rate in bytes per hour.
	Hint *writesHint `json:"hint,omitempty"`
}

type writesHint struct {
	BytesPerHour uint64   `json:"bytes_per_hour"`
	Seconds      int64    `json:"seconds"`
	LimitPerHour uint64   `json:"limit_bytes_per_hour"`
	TopUnits     []string `json:"top_units"`
}

type writesView struct {
	At time.Time `json:"at"`
	// UptimeS is how long the system has been up: the span of the disks' totals
	UptimeS   int64           `json:"uptime_s"`
	IntervalS int             `json:"interval_s"`
	Recording writesRecording `json:"recording"`
	Disks     []writesDisk    `json:"disks"`
	Units     []iowatch.Unit  `json:"units"`
}

func (a *SystemAPI) recordingState(v iowatch.View) writesRecording {
	return writesRecording{
		Supported: v.Supported && a.IOAccounting != nil,
		Enabled:   a.IOAccounting.Enabled(),
		Active:    v.Active,
		Since:     v.Since,
	}
}

func (a *SystemAPI) writesView() writesView {
	var v iowatch.View
	if a.IOWatch != nil {
		v = a.IOWatch.View()
	} else {
		v = iowatch.View{At: time.Now(), Units: []iowatch.Unit{}}
	}
	out := writesView{At: v.At, UptimeS: a.Root.ReadStatus().UptimeS, IntervalS: v.IntervalS, Recording: a.recordingState(v), Disks: []writesDisk{}, Units: v.Units}
	type known struct {
		kind, vendor, model string
		parts               map[string][2]string
	}
	disks := map[string]known{}
	for _, d := range a.Root.StatusStorage(v.At) {
		k := known{kind: d.Kind, vendor: d.Vendor, model: d.Model, parts: map[string][2]string{}}
		for _, p := range d.Partitions {
			k.parts[p.Name] = [2]string{p.Mount, p.Role}
		}
		disks[d.Name] = k
	}
	for _, d := range v.Disks {
		k := disks[d.Name]
		wd := writesDisk{Name: d.Name, Kind: k.kind, Vendor: k.vendor, Model: k.model, ReadBytes: d.ReadBytes, WrittenBytes: d.WrittenBytes, Hour: d.Hour, Day: d.Day, Partitions: []writesPartition{}}
		for _, p := range d.Partitions {
			m := k.parts[p.Name]
			wd.Partitions = append(wd.Partitions, writesPartition{Partition: p, Mount: m[0], Role: m[1]})
		}
		// a filesystem on the whole disk
		if m, ok := k.parts[d.Name]; ok && len(wd.Partitions) == 0 {
			wd.Partitions = append(wd.Partitions, writesPartition{Partition: iowatch.Partition{Name: d.Name, ReadBytes: d.ReadBytes, WrittenBytes: d.WrittenBytes}, Mount: m[0], Role: m[1]})
		}
		wd.Hint = writeHint(wd, v.Units)
		out.Disks = append(out.Disks, wd)
	}
	return out
}

// writeHint is the hint for an SD card or eMMC written to faster than WriteHintBytesPerHour, with
// up to three units that wrote the most to it in that time.
func writeHint(d writesDisk, units []iowatch.Unit) *writesHint {
	if (d.Kind != "sd" && d.Kind != "emmc") || d.Day.Seconds < writeHintMinSeconds {
		return nil
	}
	rate := uint64(float64(d.Day.WrittenBytes) * 3600 / float64(d.Day.Seconds))
	if rate < WriteHintBytesPerHour {
		return nil
	}
	h := &writesHint{BytesPerHour: rate, Seconds: d.Day.Seconds, LimitPerHour: WriteHintBytesPerHour, TopUnits: []string{}}
	type top struct {
		unit string
		w    uint64
	}
	var tops []top
	for _, u := range units {
		for _, ud := range u.Disks {
			if ud.Disk == d.Name && ud.Day.WrittenBytes > 0 {
				tops = append(tops, top{u.Unit, ud.Day.WrittenBytes})
			}
		}
	}
	sort.SliceStable(tops, func(i, j int) bool { return tops[i].w > tops[j].w })
	for i := 0; i < len(tops) && i < 3; i++ {
		h.TopUnits = append(h.TopUnits, tops[i].unit)
	}
	return h
}

func (a *SystemAPI) storageWrites(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.writesView())
}

func (a *SystemAPI) recordingNow() writesRecording {
	if a.IOWatch == nil {
		return a.recordingState(iowatch.View{})
	}
	return a.recordingState(a.IOWatch.View())
}

func (a *SystemAPI) storageWritesRecording(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.recordingNow())
}

func (a *SystemAPI) storageWritesRecordingPut(w http.ResponseWriter, r *http.Request) {
	if a.IOAccounting == nil || a.IOWatch == nil {
		writeJSON(w, http.StatusNotImplemented, apiError{Error: "not-systemd", Message: "recording disk writes per service needs systemd"})
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil {
		badBody(w, err)
		return
	}
	if body.Enabled == nil {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: "invalid-body", Message: "enabled is required"})
		return
	}
	if *body.Enabled && !a.IOWatch.Supported() {
		writeJSON(w, http.StatusConflict, apiError{Error: "unsupported", Message: "this system's kernel does not offer the io controller (in a container: the host does not pass it on)"})
		return
	}
	if err := a.IOAccounting.Switch(r.Context(), *body.Enabled); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: "internal", Message: err.Error()})
		return
	}
	slog.Info("services: the recording of disk writes per service was switched", "enabled", *body.Enabled)
	if *body.Enabled {
		a.IOWatch.Poll() // the first reading at once: the start of the recording
	} else {
		a.IOWatch.Reset()
	}
	writeJSON(w, 200, a.recordingNow())
}
