package jobs

import (
	"testing"
	"time"
)

// backupAt builds a backupObject the way pruneBackups would after parsing
// an R2 listing - key doesn't need to be realistic, just unique and stable
// enough to assert against.
func backupAt(id string, t time.Time) backupObject {
	return backupObject{key: "backups/" + id, t: t}
}

func TestBackupsToPruneKeepsEverythingInsideDailyWindow(t *testing.T) {
	now := time.Now()
	backups := []backupObject{
		backupAt("today", now),
		backupAt("10-days-ago", now.AddDate(0, 0, -10)),
		backupAt("29-days-ago", now.AddDate(0, 0, -29)),
	}

	if got := backupsToPrune(backups, now); len(got) != 0 {
		t.Errorf("backupsToPrune() = %v, want none pruned", got)
	}
}

func TestBackupsToPruneThinsWeeklyPastDailyWindow(t *testing.T) {
	// Fixed rather than time.Now()-relative, same reasoning as the
	// monthly-thinning test below: keeps "older"/"newer" reliably in the
	// same ISO week regardless of what day this test happens to run.
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	// Two backups landing in the same ISO week, both past the 30-day
	// daily-retention window but inside the 6-month weekly window -
	// only the older of the pair should be pruned.
	older := now.AddDate(0, 0, -40)
	newer := older.AddDate(0, 0, 1)
	backups := []backupObject{
		backupAt("older", older),
		backupAt("newer", newer),
	}

	toDelete := backupsToPrune(backups, now)
	if len(toDelete) != 1 || toDelete[0] != "backups/older" {
		t.Errorf("backupsToPrune() = %v, want [backups/older]", toDelete)
	}
}

func TestBackupsToPruneThinsMonthlyPastSixMonths(t *testing.T) {
	// Fixed rather than time.Now()-relative: a 7-months-ago date computed
	// from "today" can land close enough to a month boundary that adding
	// a day pushes it into the next calendar month, which would put
	// "older" and "newer" in different monthly buckets and defeat the
	// point of this test. The 15th is never within a day of a boundary.
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	// Two backups in the same calendar month, both older than 6 months -
	// only the older of the pair should be pruned.
	older := now.AddDate(0, -7, 0)
	newer := older.AddDate(0, 0, 1)
	backups := []backupObject{
		backupAt("older", older),
		backupAt("newer", newer),
	}

	toDelete := backupsToPrune(backups, now)
	if len(toDelete) != 1 || toDelete[0] != "backups/older" {
		t.Errorf("backupsToPrune() = %v, want [backups/older]", toDelete)
	}
}

func TestParseBackupTimeRoundTripsDumpDatabaseFormat(t *testing.T) {
	want := time.Date(2026, 8, 31, 20, 0, 30, 0, time.UTC)
	key := backupPrefix + "backup-" + want.Format("2006-01-02-15-04-05") + ".sql"

	got, err := parseBackupTime(key)
	if err != nil {
		t.Fatalf("parseBackupTime() error = %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("parseBackupTime() = %v, want %v", got, want)
	}
}
