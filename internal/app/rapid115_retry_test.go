package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRapid115MonitorRetryScheduleAndOutcomes(t *testing.T) {
	for _, successAt := range []int{0, 1, 2, 3, 4} {
		t.Run(string(rune('0'+successAt)), func(t *testing.T) {
			a, job := rapid115Fixture(t)
			job.FailureAction, job.SuccessAction = "upload", "keep"
			now := time.Now()
			a.rapid115Now = func() time.Time { return now }
			file := filepath.Join(job.Source, "film.mkv")
			rapid115Put(t, file, "media")
			os.Chtimes(file, now.Add(-time.Minute), now.Add(-time.Minute))
			a.store.update(func(st *State) error {
				st.Rapid115.Watches = []Rapid115Watch{{Rapid115Job: job, ID: "watch", Name: "retry", Mode: "cron", Cron: "0 0 * * *", Enabled: true}}
				return nil
			})
			calls := 0
			a.rapid115Upload = func(_ context.Context, _ Storage, _, _ string, _ *os.File, _ int64, allow bool) (bool, string, error) {
				retry := calls
				calls++
				if allow != (retry == 3) {
					t.Fatal("upload must be unavailable before retry 3", retry, allow)
				}
				if retry == successAt {
					return true, "pick", nil
				}
				if allow {
					return false, "uploaded", nil
				}
				return false, "", nil
			}
			revision := rapid115Stamp(a.store.snapshot().Rapid115)
			for retry := 0; retry < 4; retry++ {
				if err := a.runRapid115(a.ctx, job, "watch", "监控 · retry", revision); err != nil {
					t.Fatal(err)
				}
				record := a.store.snapshot().Rapid115Records[0]
				if retry == successAt || retry == 3 {
					want := "秒传成功"
					if retry > 0 {
						want = fmt.Sprintf("重试%d次秒传成功", retry)
					}
					if successAt == 4 {
						want = "传输成功"
					}
					if record.Message != want || len(a.store.snapshot().Rapid115Retries) != 0 {
						t.Fatal(record, want)
					}
					break
				}
				wait := []time.Duration{10 * time.Minute, 30 * time.Minute, 60 * time.Minute}[retry]
				if record.Status != "retrying" || !record.NextRetry.Equal(now.Add(wait)) {
					t.Fatal("incorrect retry delay", record)
				}
				previous := calls
				if err := a.runRapid115(a.ctx, job, "watch", "监控 · retry", revision); err != nil || calls != previous {
					t.Fatal("retried too early", err)
				}
				now = record.NextRetry
			}
		})
	}
}

func TestRapid115CronSkipRetriesKnownMissButNotUnknownWrite(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprint(uncertain), func(t *testing.T) {
			a, job := rapid115Fixture(t)
			job.SuccessAction = "keep"
			file := filepath.Join(job.Source, "film.mkv")
			rapid115Put(t, file, "media")
			old := time.Now().Add(-time.Minute)
			os.Chtimes(file, old, old)
			a.store.update(func(st *State) error {
				st.Rapid115.Watches = []Rapid115Watch{{Rapid115Job: job, ID: "cron", Name: "cron", Mode: "cron", Cron: "* * * * *", Enabled: true}}
				return nil
			})
			calls := 0
			a.rapid115Upload = func(context.Context, Storage, string, string, *os.File, int64, bool) (bool, string, error) {
				calls++
				if uncertain {
					return false, "", errors.New("unknown upstream result")
				}
				if calls == 1 {
					return false, "", nil
				}
				return true, "pick", nil
			}
			revision := rapid115Stamp(a.store.snapshot().Rapid115)
			for range 3 {
				if err := a.runRapid115(a.ctx, job, "cron", "监控 · cron", revision); err != nil {
					t.Fatal(err)
				}
			}
			want := 2
			if uncertain {
				want = 1
			}
			if calls != want {
				t.Fatal("wrong cron retry/uncertain replay policy", calls)
			}
		})
	}
}

func TestRapid115BlacklistPrunesBeforeEntriesAndHashes(t *testing.T) {
	a, job := rapid115Fixture(t)
	job.SuccessAction = "keep"
	job.Filters = []BackupFilter{{Type: "name", Mode: "exclude", Value: "blocked", MatchDir: true, MatchFile: true}}
	os.Mkdir(filepath.Join(job.Source, "blocked"), 0700)
	for _, name := range []string{"blocked/inside.mkv", "blocked-file.mkv", "allowed.mkv"} {
		rapid115Put(t, filepath.Join(job.Source, name), "media")
	}
	a.store.update(func(st *State) error { st.Rapid115.Manual = job; return nil })
	names := []string{}
	a.rapid115Upload = func(_ context.Context, _ Storage, _, name string, _ *os.File, _ int64, _ bool) (bool, string, error) {
		names = append(names, name)
		return true, "pick", nil
	}
	if err := a.runRapid115(a.ctx, job, "manual", "手动", rapid115Stamp(a.store.snapshot().Rapid115)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "allowed.mkv" {
		t.Fatal("blacklist reached hashing/upload", names)
	}
}

func TestRapid115WaitingRetrySurvivesRestartButInflightDoesNotReplay(t *testing.T) {
	a, job := rapid115Fixture(t)
	job.FailureAction = "upload"
	file := filepath.Join(job.Source, "film.mkv")
	rapid115Put(t, file, "media")
	old := time.Now().Add(-time.Minute)
	os.Chtimes(file, old, old)
	a.store.update(func(st *State) error {
		st.Rapid115.Watches = []Rapid115Watch{{Rapid115Job: job, ID: "watch", Name: "watch", Mode: "cron", Cron: "0 0 * * *", Enabled: true}}
		return nil
	})
	a.rapid115Upload = func(context.Context, Storage, string, string, *os.File, int64, bool) (bool, string, error) {
		return false, "", nil
	}
	if err := a.runRapid115(a.ctx, job, "watch", "监控 · watch", rapid115Stamp(a.store.snapshot().Rapid115)); err != nil {
		t.Fatal(err)
	}
	reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal(err)
	}
	st := reopened.store.snapshot()
	if len(st.Rapid115Retries) != 1 {
		t.Fatal("definitive waiting retry was lost")
	}
	key := ""
	for k := range st.Rapid115Retries {
		key = k
	}
	if fresh, err := reopened.rapid115Intent(key, Rapid115Record{ID: "inflight", Status: "running", Name: "film.mkv"}); err != nil || !fresh {
		t.Fatal(fresh, err)
	}
	second, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.store.snapshot().Rapid115Retries) != 0 || !second.store.snapshot().Rapid115Attempts[key] {
		t.Fatal("inflight retry became replayable")
	}
}

func TestRapid115DueRetryRunsBeforeNextCron(t *testing.T) {
	a, job := rapid115Fixture(t)
	ctx, cancel := context.WithCancel(a.ctx)
	a.ctx = ctx
	defer func() { cancel(); a.wg.Wait() }()
	job.FailureAction, job.SuccessAction = "upload", "keep"
	file := filepath.Join(job.Source, "film.mkv")
	rapid115Put(t, file, "media")
	old := time.Now().Add(-time.Minute)
	os.Chtimes(file, old, old)
	if err := a.store.update(func(st *State) error {
		st.Rapid115.Watches = []Rapid115Watch{{Rapid115Job: job, ID: "watch", Name: "future cron", Mode: "cron", Cron: "0 0 1 1 *", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.rapid115Upload = func(context.Context, Storage, string, string, *os.File, int64, bool) (bool, string, error) {
		return false, "", nil
	}
	if err := a.runRapid115(a.ctx, job, "watch", "监控 · future cron", rapid115Stamp(a.store.snapshot().Rapid115)); err != nil {
		t.Fatal(err)
	}
	if err := a.store.update(func(st *State) error {
		for key, retry := range st.Rapid115Retries {
			retry.Next = time.Now().Add(-time.Minute)
			st.Rapid115Retries[key] = retry
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.rapid115Upload = func(context.Context, Storage, string, string, *os.File, int64, bool) (bool, string, error) {
		return true, "pick", nil
	}
	a.wg.Add(1)
	go a.rapid115Monitor()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		st := a.store.snapshot()
		if len(st.Rapid115Records) == 2 && st.Rapid115Records[0].Message == "重试1次秒传成功" && len(st.Rapid115Retries) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("retry waited for the next Cron instead of its own deadline", a.store.snapshot().Rapid115Records)
}

func TestRapid115KnownPrewriteFailureRetriesAndSettingsCancelWaiting(t *testing.T) {
	a, job := rapid115Fixture(t)
	job.FailureAction, job.SuccessAction = "upload", "keep"
	file := filepath.Join(job.Source, "film.mkv")
	rapid115Put(t, file, "media")
	old := time.Now().Add(-time.Minute)
	os.Chtimes(file, old, old)
	if err := a.store.update(func(st *State) error {
		st.Rapid115.Watches = []Rapid115Watch{{Rapid115Job: job, ID: "watch", Name: "watch", Mode: "cron", Cron: "0 0 * * *", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.rapid115Upload = func(context.Context, Storage, string, string, *os.File, int64, bool) (bool, string, error) {
		return false, "", &rapid115KnownFailure{errors.New("prewrite authorization unavailable")}
	}
	if err := a.runRapid115(a.ctx, job, "watch", "监控 · watch", rapid115Stamp(a.store.snapshot().Rapid115)); err != nil {
		t.Fatal(err)
	}
	st := a.store.snapshot()
	if len(st.Rapid115Retries) != 1 || st.Rapid115Records[0].Status != "retrying" {
		t.Fatal("safe prewrite failure did not schedule a retry")
	}
	value := st.Rapid115
	value.Watches[0].Enabled = false
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	response := request(t, h, "PUT", "/api/115-rapid", value, cookie)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	st = a.store.snapshot()
	if len(st.Rapid115Retries) != 0 || st.Rapid115Records[0].Status != "interrupted" || !st.Rapid115Records[0].NextRetry.IsZero() {
		t.Fatal("disabled monitor left an active retry", st.Rapid115Records)
	}
}
