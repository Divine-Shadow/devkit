package worktrees

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makePersistentResetFixture(t *testing.T) (NativeSlotResetOptions, NativeResetOptions) {
	return makeNamedPersistentResetFixture(t, "host", "fixture")
}

func makeNamedPersistentResetFixture(t *testing.T, parent, repo string) (NativeSlotResetOptions, NativeResetOptions) {
	t.Helper()
	root := filepath.Join(t.TempDir(), parent)
	slot := NativeSlotResetOptions{Project: "dev-all", Repo: repo, Index: 1, Count: 1,
		WorktreeRoot: filepath.Join(root, "worktrees"), StateRoot: filepath.Join(root, "state")}
	for _, path := range []string{
		filepath.Join(slot.WorktreeRoot, "agent1", slot.Repo),
		filepath.Join(slot.StateRoot, "dev-all-agent1"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "disposable"), []byte("execution"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return slot, NativeResetOptions{Project: slot.Project, Repo: slot.Repo, Count: slot.Count,
		WorktreeRoot: slot.WorktreeRoot, StateRoot: slot.StateRoot}
}

func writeProtectedResetData(t *testing.T, home string) (string, os.FileInfo) {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(home, "goals_1.sqlite")
	persistentSQLite(t, db, "CREATE TABLE thread_goals(objective TEXT); INSERT INTO thread_goals VALUES('keep the actual task');")
	info, err := os.Stat(db)
	if err != nil {
		t.Fatal(err)
	}
	return db, info
}

func assertProtectedResetData(t *testing.T, db string, before os.FileInfo) {
	t.Helper()
	if got := persistentSQLite(t, db, "PRAGMA quick_check; SELECT objective FROM thread_goals;"); got != "ok\nkeep the actual task" {
		t.Fatalf("protected data changed: %q", got)
	}
	info, err := os.Stat(db)
	if err != nil || !os.SameFile(before, info) {
		t.Fatalf("protected database was replaced: %v", err)
	}
}

func requireLegacyResetRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "requires environment-owned migration") {
		t.Fatalf("expected protected legacy-data refusal, got %v", err)
	}
}

func TestNativeResetRechecksLegacyDataAfterPlanningAndPreparation(t *testing.T) {
	for _, phase := range []string{"selected-stage", "whole-stage", "prepare", "prepared-stage"} {
		t.Run(phase, func(t *testing.T) {
			slot, whole := makePersistentResetFixture(t)
			var plan *NativeResetPlan
			var err error
			if phase == "whole-stage" {
				plan, err = PlanNativeReset(whole)
			} else {
				plan, err = PlanNativeSlotReset(slot)
			}
			if err != nil {
				t.Fatal(err)
			}
			var transaction *NativeResetTransaction
			if phase == "prepared-stage" {
				transaction, err = plan.PrepareTransaction()
				if err != nil {
					t.Fatal(err)
				}
			}
			home := filepath.Join(slot.StateRoot, "dev-all-agent1", "home")
			db, before := writeProtectedResetData(t, home)
			switch phase {
			case "prepare":
				_, err = plan.PrepareTransaction()
			case "prepared-stage":
				err = transaction.StagePrepared()
			default:
				_, err = plan.Stage()
			}
			requireLegacyResetRefusal(t, err)
			assertProtectedResetData(t, db, before)
			for _, path := range []string{filepath.Join(slot.WorktreeRoot, "agent1", slot.Repo, "disposable"), filepath.Join(slot.StateRoot, "dev-all-agent1", "disposable")} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("refusal changed active execution before staging: %v", err)
				}
			}
		})
	}
}

func TestNativeWholeResetProtectsOtherRepositoryLegacyHome(t *testing.T) {
	slot, whole := makePersistentResetFixture(t)
	other := filepath.Join(slot.WorktreeRoot, "agent1", "other-repo", ".devhome-agent1")
	db, before := writeProtectedResetData(t, other)
	_, err := PlanNativeReset(whole)
	requireLegacyResetRefusal(t, err)
	assertProtectedResetData(t, db, before)
}

func TestNativePreparedResetProtectsMappedDataAndStillAllowsRecoveryRollback(t *testing.T) {
	slot, whole := makePersistentResetFixture(t)
	plan, err := PlanNativeSlotReset(slot)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := plan.PrepareTransaction()
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.StagePrepared(); err != nil {
		t.Fatal(err)
	}
	state := transaction.State()
	selectedState := filepath.Join(slot.StateRoot, "dev-all-agent1")
	var stagedState string
	for _, item := range state.Staged {
		if item.Source == selectedState {
			stagedState = item.Target
		}
	}
	if stagedState == "" {
		t.Fatal("real staging did not include selected execution state")
	}
	db, before := writeProtectedResetData(t, filepath.Join(stagedState, "home"))
	requireLegacyResetRefusal(t, transaction.Commit())
	assertProtectedResetData(t, db, before)
	for _, scope := range []string{"selected", "whole"} {
		var fresh *NativeResetPlan
		if scope == "selected" {
			fresh, err = PlanNativeSlotReset(slot)
		} else {
			fresh, err = PlanNativeReset(whole)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := fresh.Apply(); err == nil || !strings.Contains(err.Error(), "transaction recovery mapping") {
			t.Fatalf("%s fresh reset deleted unmapped protected quarantine: %v", scope, err)
		}
		assertProtectedResetData(t, db, before)
	}
	// Recovery validates restoration geometry without admitting deletion of the
	// protected payload. The same retained bytes can be returned to their name.
	fresh, err := PlanNativeSlotReset(slot)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := ResumeNativeResetTransaction(fresh, state)
	if err != nil {
		t.Fatal(err)
	}
	requireLegacyResetRefusal(t, recovered.Commit())
	if err := recovered.Rollback(); err != nil {
		t.Fatalf("protected payload prevented safe rollback: %v", err)
	}
	assertProtectedResetData(t, filepath.Join(selectedState, "home", filepath.Base(db)), before)
}

func TestNativeWholeResetProtectsMappedWildcardRepositoryHome(t *testing.T) {
	slot, whole := makePersistentResetFixture(t)
	other := filepath.Join(slot.WorktreeRoot, "agent1", "other-repo")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanNativeReset(whole)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := plan.Stage()
	if err != nil {
		t.Fatal(err)
	}
	var mapped string
	for _, item := range transaction.State().Staged {
		if item.Source == other {
			mapped = item.Target
		}
	}
	if mapped == "" {
		t.Fatal("whole reset did not stage the other repository")
	}
	db, before := writeProtectedResetData(t, filepath.Join(mapped, ".devhome-agent1"))
	requireLegacyResetRefusal(t, transaction.Commit())
	assertProtectedResetData(t, db, before)
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertProtectedResetData(t, filepath.Join(other, ".devhome-agent1", filepath.Base(db)), before)
}

func TestNativeResetRollsBackLegacyDataCreatedWhileStaging(t *testing.T) {
	slot, _ := makePersistentResetFixture(t)
	plan, err := PlanNativeSlotReset(slot)
	if err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(slot.WorktreeRoot, "agent1", slot.Repo)
	var db string
	var before os.FileInfo
	original := nativeResetRename
	t.Cleanup(func() { nativeResetRename = original })
	injected := false
	nativeResetRename = func(source, target string) error {
		if source == worktree && !injected {
			injected = true
			db, before = writeProtectedResetData(t, filepath.Join(source, ".devhome-agent1"))
		}
		return os.Rename(source, target)
	}
	_, err = plan.Stage()
	requireLegacyResetRefusal(t, err)
	if !injected {
		t.Fatal("real staging did not exercise the late-data race")
	}
	assertProtectedResetData(t, db, before)
}

func TestNativeFreshResetPreservesUnmappedLegacyAndCurrentQuarantine(t *testing.T) {
	for _, kind := range []string{"legacy-agent", "legacy-state", "selected-named", "selected-canonical"} {
		t.Run(kind, func(t *testing.T) {
			slot, _ := makePersistentResetFixture(t)
			boundary := filepath.Join(slot.WorktreeRoot, "agent1")
			name := ".devkit-reset-12345"
			switch kind {
			case "legacy-state":
				boundary = slot.StateRoot
			case "selected-named":
				name = ".devkit-reset-dev-all-agent1-stale"
			case "selected-canonical":
				boundary = slot.StateRoot
				name = ".devkit-reset-dev-all-agent1-" + strings.Repeat("a", 32)
			}
			quarantine := filepath.Join(boundary, name)
			db, before := writeProtectedResetData(t, filepath.Join(quarantine, "000", "home"))
			plan, err := PlanNativeSlotReset(slot)
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.Apply(); err == nil || !strings.Contains(err.Error(), "transaction recovery mapping") {
				t.Fatalf("reset acquired disposal authority from quarantine name: %v", err)
			}
			assertProtectedResetData(t, db, before)
			if got, err := os.ReadFile(filepath.Join(slot.WorktreeRoot, "agent1", slot.Repo, "disposable")); err != nil || string(got) != "execution" {
				t.Fatalf("unmapped-quarantine refusal changed selected execution: %q %v", got, err)
			}
		})
	}
}

func TestNativeResetTreatsRootAndRepositoryMetacharactersLiterally(t *testing.T) {
	for _, scope := range []string{"selected", "whole"} {
		for _, repo := range []string{"fixture[1]", "fixture?", "fixture*"} {
			t.Run(scope+"/"+repo, func(t *testing.T) {
				slot, whole := makeNamedPersistentResetFixture(t, "literal[host]*?", repo)
				var plan *NativeResetPlan
				var err error
				if scope == "selected" {
					plan, err = PlanNativeSlotReset(slot)
				} else {
					plan, err = PlanNativeReset(whole)
				}
				if err != nil {
					t.Fatal(err)
				}
				transaction, err := plan.Stage()
				if err != nil {
					t.Fatal(err)
				}
				worktree := filepath.Join(slot.WorktreeRoot, "agent1", repo)
				var mapped string
				for _, item := range transaction.State().Staged {
					if item.Source == worktree {
						mapped = item.Target
					}
				}
				if mapped == "" {
					t.Fatal("real staging did not move the literal repository")
				}
				db, before := writeProtectedResetData(t, filepath.Join(mapped, ".devhome-agent1"))
				requireLegacyResetRefusal(t, transaction.Commit())
				assertProtectedResetData(t, db, before)
				if scope == "selected" {
					fresh, err := PlanNativeSlotReset(slot)
					if err != nil {
						t.Fatal(err)
					}
					if err := fresh.Apply(); err == nil || !strings.Contains(err.Error(), "transaction recovery mapping") {
						t.Fatalf("literal root defeated fresh quarantine protection: %v", err)
					}
				}
				if err := transaction.Rollback(); err != nil {
					t.Fatal(err)
				}
				assertProtectedResetData(t, filepath.Join(worktree, ".devhome-agent1", filepath.Base(db)), before)
			})
		}
	}
}

func TestNativeResetDisposesOnlyEmptyWorktreeHomeScaffolding(t *testing.T) {
	for _, scope := range []string{"selected", "whole"} {
		t.Run(scope, func(t *testing.T) {
			slot, whole := makePersistentResetFixture(t)
			legacy := filepath.Join(slot.WorktreeRoot, "agent1", slot.Repo, ".devhome-agent1")
			if err := os.Mkdir(legacy, 0755); err != nil {
				t.Fatal(err)
			}
			persistent := filepath.Join(slot.StateRoot, ".homes", "dev-all-agent1", slot.Repo)
			database, before := writeProtectedResetData(t, persistent)
			var plan *NativeResetPlan
			var err error
			if scope == "selected" {
				plan, err = PlanNativeSlotReset(slot)
			} else {
				plan, err = PlanNativeReset(whole)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.Apply(); err != nil {
				t.Fatal(err)
			}
			assertProtectedResetData(t, database, before)
		})
	}
}

func TestNativeResetRefusesLateDataInMappedEmptyHomeAndRollsBack(t *testing.T) {
	slot, _ := makePersistentResetFixture(t)
	source := filepath.Join(slot.WorktreeRoot, "agent1", slot.Repo)
	legacy := filepath.Join(source, ".devhome-agent1")
	if err := os.Mkdir(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanNativeSlotReset(slot)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := plan.Stage()
	if err != nil {
		t.Fatal(err)
	}
	var mapped string
	for _, item := range transaction.State().Staged {
		if item.Source == source {
			mapped = filepath.Join(item.Target, ".devhome-agent1")
		}
	}
	if mapped == "" {
		t.Fatal("actual reset did not retain source-to-quarantine mapping")
	}
	database, before := writeProtectedResetData(t, mapped)
	requireLegacyResetRefusal(t, transaction.Commit())
	assertProtectedResetData(t, database, before)
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertProtectedResetData(t, filepath.Join(legacy, filepath.Base(database)), before)
}
