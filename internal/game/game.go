package game

import (
	"errors"
	"math"
	"sort"
)

type Side string

const (
	Allies Side = "allies"
	Axis   Side = "axis"
)

func (s Side) Opponent() Side {
	if s == Allies {
		return Axis
	}
	return Allies
}

type Phase string

const (
	Waiting  Phase = "waiting"
	Playing  Phase = "playing"
	Finished Phase = "finished"
)

type UnitType string

const (
	Rifleman UnitType = "rifleman"
	Assault  UnitType = "assault"
	Gunner   UnitType = "gunner"
	Tank     UnitType = "tank"
)

type UnitSpec struct {
	Name        string  `json:"name"`
	Cost        float64 `json:"cost"`
	MaxHP       float64 `json:"maxHp"`
	Speed       float64 `json:"speed"`
	Range       float64 `json:"range"`
	Damage      float64 `json:"damage"`
	Cooldown    float64 `json:"cooldown"`
	Radius      float64 `json:"radius"`
	Description string  `json:"description"`
}

var Catalog = map[UnitType]UnitSpec{
	Rifleman: {
		Name: "Rifleman", Cost: 60, MaxHP: 100, Speed: 58, Range: 175,
		Damage: 17, Cooldown: 0.9, Radius: 13,
		Description: "Infanteri serbaguna dengan jarak tembak menengah.",
	},
	Assault: {
		Name: "Assault", Cost: 90, MaxHP: 135, Speed: 82, Range: 78,
		Damage: 31, Cooldown: 0.68, Radius: 14,
		Description: "Pasukan cepat untuk menembus garis dan parit lawan.",
	},
	Gunner: {
		Name: "Gunner", Cost: 135, MaxHP: 165, Speed: 39, Range: 245,
		Damage: 12, Cooldown: 0.27, Radius: 16,
		Description: "Penekan jarak jauh dengan laju tembak tinggi.",
	},
	Tank: {
		Name: "Tank", Cost: 320, MaxHP: 590, Speed: 31, Range: 275,
		Damage: 62, Cooldown: 1.45, Radius: 26,
		Description: "Unit berat yang mahal, lambat, dan sangat tahan.",
	},
}

type PlayerState struct {
	Resources float64 `json:"resources"`
	BaseHP    float64 `json:"baseHp"`
	MaxBaseHP float64 `json:"maxBaseHp"`
}

type Unit struct {
	ID          int64    `json:"id"`
	Type        UnitType `json:"type"`
	Side        Side     `json:"side"`
	X           float64  `json:"x"`
	Y           float64  `json:"y"`
	HP          float64  `json:"hp"`
	MaxHP       float64  `json:"maxHp"`
	Radius      float64  `json:"radius"`
	AttackPulse int64    `json:"attackPulse"`
	cooldown    float64
}

type Trench struct {
	X     float64 `json:"x"`
	Width float64 `json:"width"`
}

type Snapshot struct {
	Phase    Phase                `json:"phase"`
	Winner   Side                 `json:"winner,omitempty"`
	Reason   string               `json:"reason,omitempty"`
	Elapsed  float64              `json:"elapsed"`
	Tick     int64                `json:"tick"`
	Width    float64              `json:"width"`
	Height   float64              `json:"height"`
	Players  map[Side]PlayerState `json:"players"`
	Units    []Unit               `json:"units"`
	Trenches []Trench             `json:"trenches"`
}

type player struct {
	Resources float64
	BaseHP    float64
}

type Game struct {
	phase    Phase
	winner   Side
	reason   string
	elapsed  float64
	tick     int64
	nextID   int64
	width    float64
	height   float64
	allies   player
	axis     player
	units    []Unit
	trenches []Trench
}

const (
	startResources  = 220.0
	resourceRate    = 24.0
	resourceCap     = 999.0
	maxBaseHP       = 2200.0
	maxUnitsPerSide = 120
)

var (
	ErrNotPlaying        = errors.New("match is not currently playing")
	ErrUnknownUnit       = errors.New("unknown unit type")
	ErrInsufficientFunds = errors.New("insufficient resources")
	ErrInvalidSide       = errors.New("invalid side")
	ErrUnitCap           = errors.New("unit cap reached")
)

func New() *Game {
	return &Game{
		phase:  Waiting,
		nextID: 1,
		width:  2000,
		height: 720,
		allies: player{Resources: startResources, BaseHP: maxBaseHP},
		axis:   player{Resources: startResources, BaseHP: maxBaseHP},
		trenches: []Trench{
			{X: 430, Width: 150},
			{X: 810, Width: 130},
			{X: 1190, Width: 130},
			{X: 1570, Width: 150},
		},
	}
}

func (g *Game) Start() {
	if g.phase == Waiting {
		g.phase = Playing
	}
}

func (g *Game) Phase() Phase { return g.phase }

func (g *Game) Forfeit(loser Side, reason string) {
	if g.phase != Playing || (loser != Allies && loser != Axis) {
		return
	}
	g.phase = Finished
	g.winner = loser.Opponent()
	g.reason = reason
}

func (g *Game) Spawn(side Side, unitType UnitType) error {
	if g.phase != Playing {
		return ErrNotPlaying
	}
	if side != Allies && side != Axis {
		return ErrInvalidSide
	}
	spec, ok := Catalog[unitType]
	if !ok {
		return ErrUnknownUnit
	}
	if g.unitCount(side) >= maxUnitsPerSide {
		return ErrUnitCap
	}

	p := g.playerFor(side)
	if p.Resources < spec.Cost {
		return ErrInsufficientFunds
	}
	p.Resources -= spec.Cost

	x := 125.0
	if side == Axis {
		x = g.width - 125
	}

	lane := float64((g.nextID%4)-1) * 21
	g.units = append(g.units, Unit{
		ID:     g.nextID,
		Type:   unitType,
		Side:   side,
		X:      x,
		Y:      g.height*0.67 + lane,
		HP:     spec.MaxHP,
		MaxHP:  spec.MaxHP,
		Radius: spec.Radius,
	})
	g.nextID++
	return nil
}

func (g *Game) Step(dt float64) {
	if g.phase != Playing || dt <= 0 {
		return
	}
	if dt > 0.25 {
		dt = 0.25
	}

	g.elapsed += dt
	g.tick++
	g.allies.Resources = math.Min(resourceCap, g.allies.Resources+resourceRate*dt)
	g.axis.Resources = math.Min(resourceCap, g.axis.Resources+resourceRate*dt)

	for i := range g.units {
		if g.units[i].HP <= 0 {
			continue
		}
		if g.units[i].cooldown > 0 {
			g.units[i].cooldown -= dt
		}
		g.stepUnit(i, dt)
	}

	alive := g.units[:0]
	for _, unit := range g.units {
		if unit.HP > 0 {
			alive = append(alive, unit)
		}
	}
	g.units = alive

	sort.SliceStable(g.units, func(i, j int) bool {
		if g.units[i].X == g.units[j].X {
			return g.units[i].ID < g.units[j].ID
		}
		return g.units[i].X < g.units[j].X
	})

	g.resolveWinner()
}

func (g *Game) stepUnit(index int, dt float64) {
	unit := &g.units[index]
	spec := Catalog[unit.Type]

	targetIndex, distance := g.nearestEnemy(index)
	if targetIndex >= 0 && distance <= spec.Range+g.units[targetIndex].Radius {
		if unit.cooldown <= 0 {
			damage := spec.Damage * g.damageMultiplierAt(g.units[targetIndex].X, g.units[targetIndex].Type)
			g.units[targetIndex].HP -= damage
			unit.cooldown = spec.Cooldown
			unit.AttackPulse++
		}
		return
	}

	baseX := 72.0
	if unit.Side == Allies {
		baseX = g.width - 72
	}
	baseDistance := math.Abs(baseX - unit.X)
	if baseDistance <= spec.Range+42 {
		if unit.cooldown <= 0 {
			target := g.playerFor(unit.Side.Opponent())
			target.BaseHP -= spec.Damage
			unit.cooldown = spec.Cooldown
			unit.AttackPulse++
		}
		return
	}

	if g.friendlyBlocked(index) {
		return
	}

	direction := 1.0
	if unit.Side == Axis {
		direction = -1
	}
	unit.X += direction * spec.Speed * dt
	unit.X = math.Max(85, math.Min(g.width-85, unit.X))
}

func (g *Game) nearestEnemy(index int) (int, float64) {
	unit := g.units[index]
	bestIndex := -1
	bestDistance := math.MaxFloat64
	for i := range g.units {
		if i == index || g.units[i].HP <= 0 || g.units[i].Side == unit.Side {
			continue
		}
		distance := math.Abs(g.units[i].X - unit.X)
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = i
		}
	}
	return bestIndex, bestDistance
}

func (g *Game) friendlyBlocked(index int) bool {
	unit := g.units[index]
	for i := range g.units {
		if i == index || g.units[i].HP <= 0 || g.units[i].Side != unit.Side {
			continue
		}
		delta := g.units[i].X - unit.X
		if unit.Side == Axis {
			delta = -delta
		}
		if delta > 0 && delta < unit.Radius+g.units[i].Radius+9 {
			return true
		}
	}
	return false
}

func (g *Game) damageMultiplierAt(x float64, targetType UnitType) float64 {
	if targetType == Tank {
		return 1
	}
	for _, trench := range g.trenches {
		if math.Abs(x-trench.X) <= trench.Width/2 {
			return 0.68
		}
	}
	return 1
}

func (g *Game) resolveWinner() {
	if g.allies.BaseHP > 0 && g.axis.BaseHP > 0 {
		return
	}
	g.phase = Finished
	switch {
	case g.allies.BaseHP <= 0 && g.axis.BaseHP <= 0:
		g.winner = "draw"
		g.reason = "both headquarters were destroyed"
	case g.axis.BaseHP <= 0:
		g.winner = Allies
		g.reason = "axis headquarters destroyed"
	default:
		g.winner = Axis
		g.reason = "allied headquarters destroyed"
	}
}

func (g *Game) unitCount(side Side) int {
	count := 0
	for i := range g.units {
		if g.units[i].HP > 0 && g.units[i].Side == side {
			count++
		}
	}
	return count
}

func (g *Game) playerFor(side Side) *player {
	if side == Allies {
		return &g.allies
	}
	return &g.axis
}

func (g *Game) Snapshot() Snapshot {
	units := make([]Unit, len(g.units))
	copy(units, g.units)
	trenches := make([]Trench, len(g.trenches))
	copy(trenches, g.trenches)
	return Snapshot{
		Phase:   g.phase,
		Winner:  g.winner,
		Reason:  g.reason,
		Elapsed: g.elapsed,
		Tick:    g.tick,
		Width:   g.width,
		Height:  g.height,
		Players: map[Side]PlayerState{
			Allies: {Resources: g.allies.Resources, BaseHP: math.Max(0, g.allies.BaseHP), MaxBaseHP: maxBaseHP},
			Axis:   {Resources: g.axis.Resources, BaseHP: math.Max(0, g.axis.BaseHP), MaxBaseHP: maxBaseHP},
		},
		Units:    units,
		Trenches: trenches,
	}
}
