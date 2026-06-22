package proxy

import (
	"testing"
	"time"
)

func TestNewStateDefaults(t *testing.T) {
	s := NewState("en")
	if s.Phase() != PhaseIdle {
		t.Fatalf("expected PhaseIdle, got %s", s.Phase())
	}
	if s.IsOnline("") {
		t.Fatal("expected server offline")
	}
	if s.IsBooting() {
		t.Fatal("expected not booting")
	}
	if s.CanStartWake("") != true {
		t.Fatal("expected CanStartWake true")
	}
	st := s.Status()
	if st.MaxPlayers != 20 {
		t.Fatalf("expected MaxPlayers 20, got %d", st.MaxPlayers)
	}
}

func TestPhaseTransitions(t *testing.T) {
	s := NewState("en")

	s.SetPhase(PhaseWakingHost)
	if s.Phase() != PhaseWakingHost {
		t.Fatal("expected PhaseWakingHost")
	}
	if !s.IsBooting() {
		t.Fatal("expected IsBooting true in PhaseWakingHost")
	}
	if s.CanStartWake("") {
		t.Fatal("expected CanStartWake false while booting")
	}

	s.SetPhase(PhaseWaitingLXC)
	if s.Phase() != PhaseWaitingLXC {
		t.Fatal("expected PhaseWaitingLXC")
	}

	s.SetPhase(PhaseStartingMC)
	if s.Phase() != PhaseStartingMC {
		t.Fatal("expected PhaseStartingMC")
	}

	s.SetOnline("")
	if s.Phase() != PhaseReady {
		t.Fatal("expected PhaseReady after SetOnline")
	}
	if !s.IsOnline("") {
		t.Fatal("expected online after SetOnline")
	}
	if s.IsBooting() {
		t.Fatal("expected not booting in PhaseReady")
	}

	s.SetOffline("")
	if s.Phase() != PhaseIdle {
		t.Fatal("expected PhaseIdle after SetOffline")
	}
	if s.IsOnline("") {
		t.Fatal("expected offline after SetOffline")
	}
}

func TestPhaseElapsed(t *testing.T) {
	s := NewState("en")
	s.SetPhase(PhaseWakingHost)
	time.Sleep(10 * time.Millisecond)
	elapsed := s.PhaseElapsed()
	if elapsed < 10*time.Millisecond {
		t.Fatalf("expected at least 10ms elapsed, got %v", elapsed)
	}
}

func TestLogs(t *testing.T) {
	s := NewState("en")
	s.Logf("test message %d", 42)
	logs := s.Logs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(logs))
	}
}

func TestLogTruncation(t *testing.T) {
	s := NewState("en")
	s.logMaxLen = 5
	for i := 0; i < 10; i++ {
		s.Logf("line %d", i)
	}
	logs := s.Logs()
	if len(logs) > 5 {
		t.Fatalf("expected at most 5 log lines, got %d", len(logs))
	}
}

func TestLangPacks(t *testing.T) {
	s := NewState("pt")
	lp := s.LangPack()
	if lp.MotdOffline == "" {
		t.Fatal("expected non-empty MotdOffline for pt")
	}

	s.SetLang("en")
	lp = s.LangPack()
	if lp.MotdOffline == "" {
		t.Fatal("expected non-empty MotdOffline for en")
	}

	s.SetLang("zz") // unknown → fallback
	lp = s.LangPack()
	if lp.MotdOffline != defaultLang.MotdOffline {
		t.Fatal("expected fallback to default lang")
	}
}

func TestPlayers(t *testing.T) {
	s := NewState("en")
	s.UpdatePlayers(3, []string{"Alice", "Bob", "Charlie"})
	st := s.Status()
	if st.Players != 3 {
		t.Fatalf("expected 3 players, got %d", st.Players)
	}
	if len(st.PlayerList) != 3 {
		t.Fatalf("expected 3-player list, got %d", len(st.PlayerList))
	}

	s.UpdatePlayers(0, nil)
	st = s.Status()
	if st.Players != 0 {
		t.Fatalf("expected 0 players, got %d", st.Players)
	}
}

func TestSetMaxPlayers(t *testing.T) {
	s := NewState("en")
	s.SetMaxPlayers(42)
	if s.Status().MaxPlayers != 42 {
		t.Fatal("expected MaxPlayers 42")
	}
}
