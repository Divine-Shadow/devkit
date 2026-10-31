package worktrees

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sourceRegistrationFixture(t *testing.T, s CurrentSource) SourceRegistrationBinding {
	t.Helper()
	return SourceRegistrationBinding{
		SchemaVersion: "devkit/current-source-registration/v1",
		WorkspaceID:   "darksteel-1-iw-source-01a17f67-20261030",
		Worktree:      s.Worktree, BaseWorktree: s.BaseWorktree, Home: s.Home,
		TargetID: s.TargetID, AgentIndex: s.Index, CodexHome: filepath.Join(s.Home, ".codex"),
		GitDirectory:       sourceTestGit(t, s.Worktree, "rev-parse", "--path-format=absolute", "--git-dir"),
		CommonDirectory:    sourceTestGit(t, s.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir"),
		RegistrationSHA256: strings.Repeat("a", 64),
	}
}

func registeredSourceFixture(t *testing.T, shared bool) (CurrentSource, string, string) {
	t.Helper()
	s, primary, remote := sourceFixture(t, shared)
	sibling := filepath.Join(filepath.Dir(s.BaseWorktree), "institutional-warrants-source-01a17f67-20261030")
	if shared {
		sourceTestGit(t, s.BaseWorktree, "worktree", "move", s.Worktree, sibling)
	} else if err := os.Rename(s.Worktree, sibling); err != nil {
		t.Fatal(err)
	}
	s.Worktree, s.TargetID = sibling, "darksteel-1"
	binding := sourceRegistrationFixture(t, s)
	var err error
	s.Registration, err = BindCurrentSourceRegistration(s, func(context.Context) (SourceRegistrationBinding, error) { return binding, nil })
	if err != nil {
		t.Fatal(err)
	}
	return s, primary, remote
}

func TestSourceRegistrationStrictWire(t *testing.T) {
	s, _, _ := registeredSourceFixture(t, false)
	binding := sourceRegistrationFixture(t, s)
	data, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(data)
	decoded, err := DecodeSourceRegistration(strings.NewReader(valid))
	if err != nil || decoded != binding {
		t.Fatalf("round trip: %#v %v", decoded, err)
	}
	for name, input := range map[string]string{
		"duplicate":    strings.TrimSuffix(valid, "}") + `,"targetId":"foreign"}`,
		"unknown":      strings.TrimSuffix(valid, "}") + `,"root":"/foreign"}`,
		"case alias":   strings.Replace(valid, `"targetId":`, `"TARGETID":`, 1),
		"missing":      strings.Replace(valid, `"agentIndex":1,`, "", 1),
		"null":         strings.Replace(valid, `"agentIndex":1`, `"agentIndex":null`, 1),
		"wrong type":   strings.Replace(valid, `"agentIndex":1`, `"agentIndex":"1"`, 1),
		"trailing":     valid + `{}`,
		"array":        `[` + valid + `]`,
		"truncated":    strings.TrimSuffix(valid, "}"),
		"oversized":    valid + strings.Repeat(" ", 32768),
		"invalid utf8": strings.Replace(valid, "darksteel", "dark\xffsteel", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeSourceRegistration(strings.NewReader(input)); err == nil {
				t.Fatal("invalid binding decoded")
			}
		})
	}
}

func TestRegisteredSiblingRequiresActualMatchingBinding(t *testing.T) {
	s, _, _ := registeredSourceFixture(t, false)
	valid := sourceRegistrationFixture(t, s)
	s.Registration = nil
	if _, err := PrepareCurrentSource(s, false, "", true); err == nil {
		t.Fatal("unbound sibling admitted")
	}
	for name, mutate := range map[string]func(*SourceRegistrationBinding){
		"schema":           func(b *SourceRegistrationBinding) { b.SchemaVersion = "other/v1" },
		"workspace":        func(b *SourceRegistrationBinding) { b.WorkspaceID = "../foreign" },
		"fingerprint":      func(b *SourceRegistrationBinding) { b.RegistrationSHA256 = strings.Repeat("A", 64) },
		"target":           func(b *SourceRegistrationBinding) { b.TargetID = "darksteel-2" },
		"agent":            func(b *SourceRegistrationBinding) { b.AgentIndex = 2 },
		"root":             func(b *SourceRegistrationBinding) { b.Worktree = s.BaseWorktree },
		"base":             func(b *SourceRegistrationBinding) { b.BaseWorktree = s.Worktree },
		"home":             func(b *SourceRegistrationBinding) { b.Home = filepath.Dir(s.Home) },
		"codex home":       func(b *SourceRegistrationBinding) { b.CodexHome = filepath.Join(s.Home, ".other") },
		"git directory":    func(b *SourceRegistrationBinding) { b.GitDirectory = s.BaseWorktree },
		"common directory": func(b *SourceRegistrationBinding) { b.CommonDirectory = s.BaseWorktree },
	} {
		t.Run(name, func(t *testing.T) {
			binding := valid
			mutate(&binding)
			candidate := s
			registration, err := BindCurrentSourceRegistration(candidate, func(context.Context) (SourceRegistrationBinding, error) { return binding, nil })
			if err == nil {
				candidate.Registration = registration
				_, err = PrepareCurrentSource(candidate, false, "", true)
			}
			if err == nil {
				t.Fatal("foreign registration admitted")
			}
		})
	}
	if _, err := BindCurrentSourceRegistration(s, nil); err == nil {
		t.Fatal("absent producer admitted")
	}
	if _, err := BindCurrentSourceRegistration(s, func(context.Context) (SourceRegistrationBinding, error) { return valid, fmt.Errorf("supplier refused") }); err == nil {
		t.Fatal("failed producer admitted")
	}
}

func TestSourceRegistrationCannotAdmitArbitraryWorkspaceGeometry(t *testing.T) {
	s, _, _ := sourceFixture(t, false)
	s.TargetID = "darksteel-1"
	binding := sourceRegistrationFixture(t, s)
	if _, err := BindCurrentSourceRegistration(s, func(context.Context) (SourceRegistrationBinding, error) { return binding, nil }); err == nil {
		t.Fatal("non-sibling binding admitted")
	}
}

func TestSourceRegistrationRejectsRedirectedAndReplacedIdentity(t *testing.T) {
	for _, variant := range []string{"symlink", "root", "git"} {
		t.Run(variant, func(t *testing.T) {
			s, _, remote := registeredSourceFixture(t, false)
			if variant == "symlink" {
				binding := sourceRegistrationFixture(t, s)
				alias := filepath.Join(filepath.Dir(s.Worktree), "alias")
				if err := os.Symlink(binding.GitDirectory, alias); err != nil {
					t.Fatal(err)
				}
				binding.GitDirectory = alias
				if _, err := BindCurrentSourceRegistration(s, func(context.Context) (SourceRegistrationBinding, error) { return binding, nil }); err == nil {
					t.Fatal("redirected Git metadata admitted")
				}
				return
			}
			if variant == "root" {
				if err := os.Rename(s.Worktree, s.Worktree+"-retained"); err != nil {
					t.Fatal(err)
				}
				sourceTestGit(t, filepath.Dir(s.Worktree), "clone", remote, s.Worktree)
			} else {
				git := filepath.Join(s.Worktree, ".git")
				if err := os.Rename(git, git+"-retained"); err != nil {
					t.Fatal(err)
				}
				other := filepath.Join(t.TempDir(), "clone")
				sourceTestGit(t, filepath.Dir(other), "clone", remote, other)
				if err := os.Rename(filepath.Join(other, ".git"), git); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := PrepareCurrentSource(s, false, "", true); err == nil {
				t.Fatal("replaced directory identity admitted")
			}
		})
	}
}

func TestSourceRegistrationChangeUnderRefLockAbortsAndSettles(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "shared"}[shared], func(t *testing.T) {
			s, primary, _ := registeredSourceFixture(t, shared)
			binding := sourceRegistrationFixture(t, s)
			common := binding.CommonDirectory
			lock := filepath.Join(common, "refs", "remotes", "origin", "main.lock")
			observedLock := false
			var err error
			s.Registration, err = BindCurrentSourceRegistration(s, func(context.Context) (SourceRegistrationBinding, error) {
				current := binding
				if _, err := os.Lstat(lock); err == nil {
					observedLock = true
					current.RegistrationSHA256 = strings.Repeat("b", 64)
				}
				return current, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			previous := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main")
			sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "advance")
			sourceTestGit(t, primary, "push", "origin", "main")
			result, err := PrepareCurrentSource(s, false, "", false)
			if err == nil || !observedLock || result.TrackingUpdate != "not-committed" {
				t.Fatalf("result %#v observed lock %v error %v", result, observedLock, err)
			}
			if got := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main"); got != previous {
				t.Fatal("changed registration updated tracking")
			}
			if _, err := os.Lstat(lock); !os.IsNotExist(err) {
				t.Fatal("aborted ref lock not settled")
			}
			// A subsequent ordinary Git transaction proves lock release without
			// removing any lock or altering the preserved tracking value.
			sourceTestGit(t, s.Worktree, "update-ref", "refs/remotes/origin/main", previous, previous)
		})
	}
}

func TestSourceRegistrationSlowLookupKeepsRefTransactionLive(t *testing.T) {
	s, primary, _ := registeredSourceFixture(t, false)
	binding := sourceRegistrationFixture(t, s)
	lock := filepath.Join(binding.CommonDirectory, "refs", "remotes", "origin", "main.lock")
	observedLock := false
	var err error
	s.Registration, err = BindCurrentSourceRegistration(s, func(ctx context.Context) (SourceRegistrationBinding, error) {
		if _, err := os.Lstat(lock); err == nil {
			observedLock = true
			// A successful owning lookup may exceed the original metadata-only
			// budget while remaining within its published lookup deadline.
			timer := time.NewTimer(nativeMetadataOperationLimit + 50*time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return SourceRegistrationBinding{}, ctx.Err()
			case <-timer.C:
			}
			if _, err := os.Lstat(lock); err != nil {
				return SourceRegistrationBinding{}, fmt.Errorf("ref lock expired during valid lookup: %w", err)
			}
		}
		return binding, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "advance")
	sourceTestGit(t, primary, "push", "origin", "main")
	want := sourceTestGit(t, primary, "rev-parse", "HEAD")
	result, err := PrepareCurrentSource(s, false, "", false)
	if err != nil || !observedLock || result.TrackingUpdate != "committed" {
		t.Fatalf("slow lookup result %#v observed lock %v error %v", result, observedLock, err)
	}
	if got := sourceTestGit(t, s.Worktree, "rev-parse", "origin/main"); got != want {
		t.Fatal("slow valid registration did not acquire accepted tracking")
	}
}

func TestSourceRegistrationLookupObeysEnclosingCancellation(t *testing.T) {
	s, _, _ := registeredSourceFixture(t, false)
	binding := sourceRegistrationFixture(t, s)
	cancelledLookup := false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Registration.lookup = func(ctx context.Context) (SourceRegistrationBinding, error) {
		cancel()
		<-ctx.Done()
		cancelledLookup = true
		return binding, nil
	}
	if err := s.Registration.validate(ctx, s, false, ""); err == nil || !cancelledLookup {
		t.Fatal("lookup accepted after enclosing cancellation")
	}
}

func TestSourceRegistrationRechecksIdentityAfterLookup(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "replaced", true: "redirected"}[redirect], func(t *testing.T) {
			s, _, _ := registeredSourceFixture(t, false)
			binding := sourceRegistrationFixture(t, s)
			s.Registration.lookup = func(context.Context) (SourceRegistrationBinding, error) {
				// Simulate an external root change while the owner reads its
				// unchanged registration. The producer itself remains read-only.
				retained := s.Worktree + "-retained"
				if err := os.Rename(s.Worktree, retained); err != nil {
					return SourceRegistrationBinding{}, err
				}
				if redirect {
					if err := os.Symlink(retained, s.Worktree); err != nil {
						return SourceRegistrationBinding{}, err
					}
				} else if err := os.Mkdir(s.Worktree, 0700); err != nil {
					return SourceRegistrationBinding{}, err
				}
				return binding, nil
			}
			if err := s.Registration.validate(context.Background(), s, false, ""); err == nil {
				t.Fatal("root changed during lookup but binding was accepted")
			}
		})
	}
}

func TestSourceRegistrationRechecksLinkedMetadataAfterLookup(t *testing.T) {
	for _, pointer := range []string{"git", "common"} {
		t.Run(pointer, func(t *testing.T) {
			s, _, _ := registeredSourceFixture(t, true)
			binding := sourceRegistrationFixture(t, s)
			foreign, _, _ := sourceFixture(t, false)
			foreignGit := sourceTestGit(t, foreign.Worktree, "rev-parse", "--path-format=absolute", "--git-dir")
			s.Registration.lookup = func(context.Context) (SourceRegistrationBinding, error) {
				path, value := filepath.Join(s.Worktree, ".git"), "gitdir: "+foreignGit+"\n"
				if pointer == "common" {
					path, value = filepath.Join(binding.GitDirectory, "commondir"), foreignGit+"\n"
				}
				if err := os.WriteFile(path, []byte(value), 0600); err != nil {
					return SourceRegistrationBinding{}, err
				}
				return binding, nil
			}
			if err := s.Registration.validate(context.Background(), s, false, ""); err == nil {
				t.Fatal("linked metadata redirected during lookup but binding accepted")
			}
		})
	}
}

func TestSourceRegistrationLinkedRedirectUnderRefLockAbortsAndSettles(t *testing.T) {
	for _, pointer := range []string{"git", "common"} {
		t.Run(pointer, func(t *testing.T) {
			s, primary, _ := registeredSourceFixture(t, true)
			binding := sourceRegistrationFixture(t, s)
			foreign, _, _ := sourceFixture(t, false)
			foreignGit := sourceTestGit(t, foreign.Worktree, "rev-parse", "--path-format=absolute", "--git-dir")
			lock := filepath.Join(binding.CommonDirectory, "refs", "remotes", "origin", "main.lock")
			observedLock := false
			var err error
			s.Registration, err = BindCurrentSourceRegistration(s, func(context.Context) (SourceRegistrationBinding, error) {
				if _, err := os.Lstat(lock); err == nil {
					observedLock = true
					path, value := filepath.Join(s.Worktree, ".git"), "gitdir: "+foreignGit+"\n"
					if pointer == "common" {
						path, value = filepath.Join(binding.GitDirectory, "commondir"), foreignGit+"\n"
					}
					if err := os.WriteFile(path, []byte(value), 0600); err != nil {
						return SourceRegistrationBinding{}, err
					}
				}
				return binding, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			previous := sourceTestGit(t, s.BaseWorktree, "rev-parse", "origin/main")
			sourceTestGit(t, primary, "commit", "--allow-empty", "-m", "advance")
			sourceTestGit(t, primary, "push", "origin", "main")
			result, err := PrepareCurrentSource(s, false, "", false)
			if err == nil || !observedLock || result.TrackingUpdate != "not-committed" {
				t.Fatalf("redirect result %#v observed lock %v error %v", result, observedLock, err)
			}
			if got := sourceTestGit(t, s.BaseWorktree, "rev-parse", "origin/main"); got != previous {
				t.Fatal("redirect changed original lane tracking")
			}
			if _, err := os.Lstat(lock); !os.IsNotExist(err) {
				t.Fatal("redirect left original ref lock behind")
			}
			sourceTestGit(t, s.BaseWorktree, "update-ref", "refs/remotes/origin/main", previous, previous)
		})
	}
}
