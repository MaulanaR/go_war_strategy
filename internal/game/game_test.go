package game

import (
	"errors"
	"testing"
)

func TestSpawnRequiresPlayingMatch(t *testing.T) {
	g := New()
	if err := g.Spawn(Allies, Rifleman); !errors.Is(err, ErrNotPlaying) {
		t.Fatalf("expected ErrNotPlaying, got %v", err)
	}
}

func TestSpawnDeductsResources(t *testing.T) {
	g := New()
	g.Start()
	before := g.allies.Resources
	if err := g.Spawn(Allies, Rifleman); err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	want := before - Catalog[Rifleman].Cost
	if g.allies.Resources != want {
		t.Fatalf("resources = %.2f, want %.2f", g.allies.Resources, want)
	}
	if len(g.units) != 1 || g.units[0].Side != Allies {
		t.Fatalf("spawned unit is incorrect: %#v", g.units)
	}
}

func TestTrenchReducesInfantryDamage(t *testing.T) {
	g := New()
	inside := g.trenches[0].X
	if got := g.damageMultiplierAt(inside, Rifleman); got >= 1 {
		t.Fatalf("expected trench protection, got multiplier %.2f", got)
	}
	if got := g.damageMultiplierAt(inside, Tank); got != 1 {
		t.Fatalf("tank should not receive trench protection, got %.2f", got)
	}
}

func TestUnitCanDestroyEnemyBase(t *testing.T) {
	g := New()
	g.Start()
	g.allies.Resources = 1000
	if err := g.Spawn(Allies, Tank); err != nil {
		t.Fatalf("spawn tank: %v", err)
	}
	g.axis.BaseHP = 10
	g.units[0].X = g.width - 100
	g.Step(0.1)
	if g.phase != Finished || g.winner != Allies {
		t.Fatalf("phase=%s winner=%s", g.phase, g.winner)
	}
}

func TestSpawnEnforcesUnitCap(t *testing.T) {
	g := New()
	g.Start()
	g.allies.Resources = 1_000_000
	for i := 0; i < maxUnitsPerSide; i++ {
		if err := g.Spawn(Allies, Rifleman); err != nil {
			t.Fatalf("spawn %d failed: %v", i, err)
		}
	}
	if err := g.Spawn(Allies, Rifleman); !errors.Is(err, ErrUnitCap) {
		t.Fatalf("expected ErrUnitCap, got %v", err)
	}
}
