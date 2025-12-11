package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"image/color"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	screenPadding      = float32(50.0)
	minimumSeparation  = float32(0.0001)
	minSpawnRadius     = float32(4.0) // Minimum radius for spawning balls
	maxSpawnRadius     = float32(120.0)
	ballSpawnStep      = 0.5
	maxCollisionSolves = 4 // Reduced max collision solves for performance
	penetrationSlop    = float32(0.001)
	waterRestDistance  = float32(12.0)
	waterInteraction   = waterRestDistance * 1.8
	waterViscosity     = float32(0.55)
	waterSpawnClampMin = float32(3.0)
	waterSpawnClampMax = float32(20.0)
	waterRestDensity   = float32(4.5)
	waterPressureStiff = float32(0.32)
	waterNearStiff     = float32(1.1)
	waterBoundaryPush  = float32(0.22)
	waterBoundaryDrag  = float32(0.05)
	gasRestDistance    = float32(16.0)
	gasInteraction     = gasRestDistance * 1.5
	gasPressure        = float32(0.12)
	gasViscosity       = float32(0.08)
	gasBuoyancy        = float32(0.25)
	gasDrag            = float32(0.05)
	gasSpawnClampMin   = float32(4.0)
	gasSpawnClampMax   = float32(30.0)
	gasBoundaryPush    = float32(0.12)
	gasBoundaryDrag    = float32(0.04)

	// Update configuration
	githubOwner = "bencewokk"
	githubRepo  = "phixgo"
	version     = "v1.0.0" // Current version
)

var (
	screenWidth, screenHeight = ebiten.ScreenSizeInFullscreen()
)

// Game settings (modifiable)
type Settings struct {
	gravity              float32
	maxSpeed             float32
	moveAwayDistance     float32
	moveAwayStrength     float32
	moveAttractStrength  float32
	groundRestitution    float32
	collisionRestitution float32
	airDrag              float32
	groundFriction       float32
	hasTopBarrier        bool
}

func defaultSettings() Settings {
	return Settings{
		gravity:              0.2,
		maxSpeed:             10.0,
		moveAwayDistance:     100.0,
		moveAwayStrength:     5.0,
		moveAttractStrength:  10.0,
		groundRestitution:    0.65,
		collisionRestitution: 0.85,
		airDrag:              0.02,
		groundFriction:       0.8,
		hasTopBarrier:        false,
	}
}

type Game struct {
	settings          Settings
	showMenu          bool
	selectedOption    int
	prevEscPressed    bool
	prevUpPressed     bool
	prevDownPressed   bool
	collider          spatialHash
	cellCache         []cellCoord
	spawnClusterCount int
	waterCollider     spatialHash
	waterCellCache    []cellCoord
	waterIndices      []int
	waterDensity      []float32
	waterNearDensity  []float32
	waterIndexMap     map[int]int
	solidCollider     spatialHash
	solidIndices      []int
	gasCollider       spatialHash
	gasCellCache      []cellCoord
	gasIndices        []int
	updateButtonHover bool
	updateChecking    bool
	updateAvailable   bool
	updateMessage     string
}

func NewGame() *Game {
	return &Game{
		settings:          defaultSettings(),
		showMenu:          false,
		collider:          newSpatialHash(maxSpawnRadius * 2),
		spawnClusterCount: 3,
		waterCollider:     newSpatialHash(waterRestDistance * 2),
		waterIndexMap:     make(map[int]int),
		solidCollider:     newSpatialHash(maxSpawnRadius * 2),
		gasCollider:       newSpatialHash(gasRestDistance * 2),
	}
}

type Pos struct {
	x, y float32
}

func createPos(x, y float32) Pos {
	return Pos{x: x, y: y}
}

type Velocity struct {
	vx, vy float32
}

type ShapeType int

const (
	ShapeCircle ShapeType = iota
	ShapeSquare
	ShapeTriangle
	ShapeWater
	ShapeGas
	ShapeStatic
)

type Ball struct {
	pos      Pos
	velocity Velocity
	radius   float32
	shape    ShapeType
	material MaterialType
}

func createBall(pos Pos, r float32, shape ShapeType) Ball {
	return Ball{pos: pos, velocity: Velocity{vx: 0, vy: 0}, radius: r, shape: shape, material: MaterialSolid}
}

type MaterialType int

const (
	MaterialSolid MaterialType = iota
	MaterialWater
	MaterialGas
	MaterialStatic
)

func createWaterParticle(pos Pos, r float32) Ball {
	b := createBall(pos, r, ShapeWater)
	b.material = MaterialWater
	return b
}

func createGasParticle(pos Pos, r float32) Ball {
	b := createBall(pos, r, ShapeGas)
	b.material = MaterialGas
	return b
}

func createStaticSolid(pos Pos, r float32, shape ShapeType) Ball {
	b := createBall(pos, r, shape)
	b.material = MaterialStatic
	return b
}

// spatialHash accelerates neighbor lookups via a uniform grid.
type spatialHash struct {
	cellSize      float32
	invCellSize   float32
	invCellSize64 float64
	buckets       map[int64][]int
	usedKeys      []int64
}

type cellCoord struct {
	x int
	y int
}

func newSpatialHash(cellSize float32) spatialHash {
	if cellSize <= 0 {
		cellSize = 1
	}
	inv := 1 / cellSize
	return spatialHash{
		cellSize:      cellSize,
		invCellSize:   inv,
		invCellSize64: float64(inv),
		buckets:       make(map[int64][]int),
	}
}

func (h *spatialHash) Clear() {
	for _, key := range h.usedKeys {
		h.buckets[key] = h.buckets[key][:0]
	}
	h.usedKeys = h.usedKeys[:0]
}

func (h *spatialHash) insert(index, ix, iy int) {
	key := hashKey(ix, iy)
	bucket := h.buckets[key]
	if bucket == nil {
		bucket = make([]int, 0, 8)
	}
	if len(bucket) == 0 {
		h.usedKeys = append(h.usedKeys, key)
	}
	bucket = append(bucket, index)
	h.buckets[key] = bucket
}

func (h *spatialHash) cell(ix, iy int) []int {
	key := hashKey(ix, iy)
	return h.buckets[key]
}

func (h *spatialHash) coord(value float32) int {
	return int(math.Floor(float64(value) * h.invCellSize64))
}

func hashKey(ix, iy int) int64 {
	return (int64(uint32(ix)) << 32) | int64(uint32(iy))
}

var neighborOffsets = [...]struct{ dx, dy int }{
	{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1},
	{1, 1}, {1, -1}, {-1, 1}, {-1, -1},
}

func (b *Ball) speed() float32 {
	return float32(math.Sqrt(float64(b.velocity.vx*b.velocity.vx + b.velocity.vy*b.velocity.vy)))
}

func (b *Ball) speedSquared() float32 {
	return b.velocity.vx*b.velocity.vx + b.velocity.vy*b.velocity.vy
}

func normalize(dx, dy float32) (nx, ny, distance float32) {
	distSq := dx*dx + dy*dy
	if distSq < minimumSeparation*minimumSeparation {
		return 0, 0, minimumSeparation
	}
	distance = float32(math.Sqrt(float64(distSq)))
	return dx / distance, dy / distance, distance
}

func mobilityFor(material MaterialType) float32 {
	if material == MaterialStatic {
		return 0
	}
	return 1
}

func resolveCollision(b1, b2 *Ball, collisionRestitution float32) bool {
	return resolveCollisionCustom(b1, b2, collisionRestitution, 0.5)
}

func resolveCollisionCustom(b1, b2 *Ball, collisionRestitution, friction float32) bool {
	dx := b2.pos.x - b1.pos.x
	dy := b2.pos.y - b1.pos.y
	combinedRadius := b1.radius + b2.radius
	combinedRadiusSq := combinedRadius * combinedRadius
	distSq := dx*dx + dy*dy
	if distSq >= combinedRadiusSq {
		return false
	}

	if distSq < minimumSeparation*minimumSeparation {
		distSq = minimumSeparation * minimumSeparation
	}

	distance := float32(math.Sqrt(float64(distSq)))
	nx := dx / distance
	ny := dy / distance
	if nx == 0 && ny == 0 {
		nx = 1
	}
	overlap := combinedRadius - distance
	if overlap <= 0 {
		return false
	}

	mob1 := mobilityFor(b1.material)
	mob2 := mobilityFor(b2.material)

	// Add a small slop to keep shapes from sinking into each other when resting.
	separation := overlap + penetrationSlop
	weight1 := mob1
	weight2 := mob2
	weightSum := weight1 + weight2
	if weightSum == 0 {
		return true
	}
	shift1 := separation * (weight1 / weightSum)
	shift2 := separation * (weight2 / weightSum)
	if mob1 > 0 {
		b1.pos.x -= nx * shift1
		b1.pos.y -= ny * shift1
	}
	if mob2 > 0 {
		b2.pos.x += nx * shift2
		b2.pos.y += ny * shift2
	}

	// Relative velocity along the normal.
	rvx := b2.velocity.vx - b1.velocity.vx
	rvy := b2.velocity.vy - b1.velocity.vy
	velAlongNormal := rvx*nx + rvy*ny
	if velAlongNormal > 0 {
		return true
	}

	restitution := collisionRestitution
	invMass1 := mob1
	invMass2 := mob2
	massSum := invMass1 + invMass2
	if massSum == 0 {
		return true
	}
	impulseScalar := -(1 + restitution) * velAlongNormal / massSum
	impulseX := impulseScalar * nx
	impulseY := impulseScalar * ny

	if invMass1 > 0 {
		b1.velocity.vx -= impulseX * invMass1
		b1.velocity.vy -= impulseY * invMass1
	}
	if invMass2 > 0 {
		b2.velocity.vx += impulseX * invMass2
		b2.velocity.vy += impulseY * invMass2
	}

	// Simple tangential friction to reduce sliding after collision.
	// Compute tangential speed and apply proportional friction impulse.
	tx := -ny
	ty := nx
	relTangential := rvx*tx + rvy*ty
	if friction != 0 {
		frictionScalar := relTangential * friction / massSum
		fx := tx * frictionScalar
		fy := ty * frictionScalar
		if invMass1 > 0 {
			b1.velocity.vx += fx * invMass1
			b1.velocity.vy += fy * invMass1
		}
		if invMass2 > 0 {
			b2.velocity.vx -= fx * invMass2
			b2.velocity.vy -= fy * invMass2
		}
	}
	return true
}

func velocityToColor(velocity float32, maxSpeed float32) color.Color {
	normalizedSpeed := velocity / maxSpeed
	if normalizedSpeed > 1 {
		normalizedSpeed = 1
	}

	g := uint8(normalizedSpeed * 255)
	b := uint8((1 - normalizedSpeed) * 255)

	return color.RGBA{R: g, G: b, B: 0, A: 255}
}

func drawShape(screen *ebiten.Image, shape ShapeType, x, y, radius float32, col color.Color) {
	switch shape {
	case ShapeCircle:
		vector.DrawFilledCircle(screen, x, y, radius, col, false)
	case ShapeSquare:
		vector.DrawFilledRect(screen, x-radius, y-radius, radius*2, radius*2, col, false)
	case ShapeTriangle:
		// Draw equilateral triangle
		height := radius * 1.732 // sqrt(3)
		path := vector.Path{}
		path.MoveTo(x, y-height*0.67)        // Top vertex
		path.LineTo(x-radius, y+height*0.33) // Bottom left
		path.LineTo(x+radius, y+height*0.33) // Bottom right
		path.Close()

		vertices, indices := path.AppendVerticesAndIndicesForFilling(nil, nil)
		for i := range vertices {
			vertices[i].ColorR = float32(col.(color.RGBA).R) / 255
			vertices[i].ColorG = float32(col.(color.RGBA).G) / 255
			vertices[i].ColorB = float32(col.(color.RGBA).B) / 255
			vertices[i].ColorA = float32(col.(color.RGBA).A) / 255
		}
		screen.DrawTriangles(vertices, indices, emptyImage, &ebiten.DrawTrianglesOptions{
			AntiAlias: false,
		})
	case ShapeWater:
		vector.DrawFilledCircle(screen, x, y, radius, col, false)
	case ShapeGas:
		vector.DrawFilledCircle(screen, x, y, radius, col, false)
	case ShapeStatic:
		vector.DrawFilledCircle(screen, x, y, radius, col, false)
	}
}

var emptyImage = ebiten.NewImage(3, 3)

const menuOptionCount = 12

var (
	ballsize            float64 = 10
	moveAttractDistance float64 = 200.0
	balls               []Ball
	ballSpawnTimer      int
	currentShape        ShapeType = ShapeCircle
)

func (g *Game) Update() error {
	// Toggle menu with ESC
	escPressed := ebiten.IsKeyPressed(ebiten.KeyEscape)
	if escPressed && !g.prevEscPressed {
		g.showMenu = !g.showMenu
	}
	g.prevEscPressed = escPressed

	// Handle menu navigation
	if g.showMenu {
		upPressed := ebiten.IsKeyPressed(ebiten.KeyUp)
		downPressed := ebiten.IsKeyPressed(ebiten.KeyDown)

		if upPressed && !g.prevUpPressed {
			g.selectedOption--
			if g.selectedOption < 0 {
				g.selectedOption = menuOptionCount - 1
			}
		}
		if downPressed && !g.prevDownPressed {
			g.selectedOption++
			if g.selectedOption > menuOptionCount-1 {
				g.selectedOption = 0
			}
		}

		g.prevUpPressed = upPressed
		g.prevDownPressed = downPressed

		// Adjust selected setting
		_, my := ebiten.Wheel()
		changeAmount := float32(0.01)
		if ebiten.IsKeyPressed(ebiten.KeyShift) {
			changeAmount = 0.1
		}

		if my != 0 {
			change := float32(my) * changeAmount
			switch g.selectedOption {
			case 0: // Gravity
				g.settings.gravity = float32(math.Max(0, float64(g.settings.gravity+change)))
			case 1: // Max Speed
				g.settings.maxSpeed = float32(math.Max(0.1, float64(g.settings.maxSpeed+change)))
			case 2: // Move Away Distance
				g.settings.moveAwayDistance = float32(math.Max(10, float64(g.settings.moveAwayDistance+change*10)))
			case 3: // Move Away Strength
				g.settings.moveAwayStrength = float32(math.Max(0.1, float64(g.settings.moveAwayStrength+change)))
			case 4: // Move Attract Strength
				g.settings.moveAttractStrength = float32(math.Max(0.1, float64(g.settings.moveAttractStrength+change)))
			case 5: // Ground Restitution
				g.settings.groundRestitution = float32(math.Min(1, math.Max(0, float64(g.settings.groundRestitution+change))))
			case 6: // Collision Restitution
				g.settings.collisionRestitution = float32(math.Min(1, math.Max(0, float64(g.settings.collisionRestitution+change))))
			case 7: // Air Drag
				g.settings.airDrag = float32(math.Min(1, math.Max(0, float64(g.settings.airDrag+change))))
			case 8: // Ground Friction
				g.settings.groundFriction = float32(math.Min(1, math.Max(0, float64(g.settings.groundFriction+change))))
			case 9: // Spawn Count
				delta := int(my)
				if ebiten.IsKeyPressed(ebiten.KeyShift) {
					delta *= 5
				}
				g.spawnClusterCount += delta
				if g.spawnClusterCount < 1 {
					g.spawnClusterCount = 1
				}
				if g.spawnClusterCount > 50 {
					g.spawnClusterCount = 50
				}
			case 10: // Top Barrier
				if my != 0 {
					g.settings.hasTopBarrier = !g.settings.hasTopBarrier
				}
			case 11: // Exit
				if my > 0 {
					return ebiten.Termination
				}
			}
		}

		return nil // Don't update physics when menu is open
	}

	// Shape selection with number keys
	if ebiten.IsKeyPressed(ebiten.Key1) {
		currentShape = ShapeCircle
	} else if ebiten.IsKeyPressed(ebiten.Key2) {
		currentShape = ShapeSquare
	} else if ebiten.IsKeyPressed(ebiten.Key3) {
		currentShape = ShapeTriangle
	} else if ebiten.IsKeyPressed(ebiten.Key4) {
		currentShape = ShapeWater
	} else if ebiten.IsKeyPressed(ebiten.Key5) {
		currentShape = ShapeGas
	} else if ebiten.IsKeyPressed(ebiten.Key6) {
		currentShape = ShapeStatic
	}

	_, my := ebiten.Wheel()

	if ebiten.IsKeyPressed(ebiten.KeyShift) {
		if my < 0 {
			moveAttractDistance += 2
		} else if my > 0 {
			moveAttractDistance -= 2
		}
	} else {
		if my < 0 {
			ballsize += ballSpawnStep
		} else if my > 0 {
			ballsize -= ballSpawnStep
		}

		// Keep ballsize within reasonable bounds and ensure it's never zero
		ballsize = math.Max(math.Min(ballsize, float64(maxSpawnRadius)), float64(minSpawnRadius))
	}

	// Handle update button click
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) && g.updateButtonHover && !g.updateChecking {
		g.updateChecking = true
		g.updateMessage = ""
		go func() {
			release, err := checkForUpdates()
			if err != nil {
				g.updateMessage = fmt.Sprintf("Error: %v", err)
				g.updateChecking = false
				return
			}
			if release == nil {
				g.updateMessage = fmt.Sprintf("Up to date! (%s)", version)
				g.updateAvailable = false
			} else {
				g.updateMessage = fmt.Sprintf("New version: %s", release.TagName)
				g.updateAvailable = true
			}
			g.updateChecking = false
		}()
	}

	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		x, y := ebiten.CursorPosition()

		if ebiten.IsKeyPressed(ebiten.KeyShift) {
			for i := len(balls) - 1; i >= 0; i-- {
				dx := balls[i].pos.x - float32(x)
				dy := balls[i].pos.y - float32(y)
				distSq := dx*dx + dy*dy

				radiusCheck := balls[i].radius + 15
				if distSq < radiusCheck*radiusCheck {
					balls = append(balls[:i], balls[i+1:]...)
				}
			}
		} else if ballSpawnTimer <= 0 {
			count := g.spawnClusterCount
			if count < 1 {
				count = 1
			}
			clampSolid := func(size float64) float32 {
				return float32(math.Min(math.Max(size, float64(minSpawnRadius)), float64(maxSpawnRadius)))
			}
			clampWater := func(size float64) float32 {
				return float32(math.Min(math.Max(size, float64(waterSpawnClampMin)), float64(waterSpawnClampMax)))
			}
			clampGas := func(size float64) float32 {
				return float32(math.Min(math.Max(size, float64(gasSpawnClampMin)), float64(gasSpawnClampMax)))
			}
			baseSolid := clampSolid(ballsize)
			baseWater := clampWater(ballsize)
			baseGas := clampGas(ballsize)
			for n := 0; n < count; n++ {
				angle := 0.0
				if count > 1 {
					angle = 2 * math.Pi * float64(n) / float64(count)
				}
				offsetScale := float32(0)
				if count > 1 {
					switch currentShape {
					case ShapeWater:
						offsetScale = baseWater * 0.5
					case ShapeGas:
						offsetScale = baseGas * 0.4
					default:
						offsetScale = baseSolid * 0.6
					}
				}
				offsetX := float32(math.Cos(angle)) * offsetScale
				offsetY := float32(math.Sin(angle)) * offsetScale
				pos := createPos(float32(x)+offsetX, float32(y)+offsetY)
				switch currentShape {
				case ShapeWater:
					balls = append(balls, createWaterParticle(pos, baseWater))
				case ShapeGas:
					balls = append(balls, createGasParticle(pos, baseGas))
				case ShapeStatic:
					balls = append(balls, createStaticSolid(pos, baseSolid, ShapeStatic))
				default:
					balls = append(balls, createBall(pos, baseSolid, currentShape))
				}
			}
			ballSpawnTimer = 3 // Spawn every 3 frames (20 times per second at 60 FPS)
		}
	}

	if ballSpawnTimer > 0 {
		ballSpawnTimer--
	}

	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonRight) {
		x, y := ebiten.CursorPosition()
		mousePos := createPos(float32(x), float32(y))

		if ebiten.IsKeyPressed(ebiten.KeyShift) {
			attractDistSq := float32(moveAttractDistance * moveAttractDistance)
			for i := range balls {
				dx := balls[i].pos.x - mousePos.x
				dy := balls[i].pos.y - mousePos.y
				distSq := dx*dx + dy*dy

				if distSq < attractDistSq {
					nx, ny, _ := normalize(dx, dy)
					balls[i].velocity.vx -= nx * g.settings.moveAttractStrength
					balls[i].velocity.vy -= ny * g.settings.moveAttractStrength
				}
			}
		} else {
			moveAwayDistSq := g.settings.moveAwayDistance * g.settings.moveAwayDistance
			for i := range balls {
				dx := balls[i].pos.x - mousePos.x
				dy := balls[i].pos.y - mousePos.y
				distSq := dx*dx + dy*dy

				if distSq < moveAwayDistSq {
					nx, ny, _ := normalize(dx, dy)
					balls[i].velocity.vx += nx * g.settings.moveAwayStrength
					balls[i].velocity.vy += ny * g.settings.moveAwayStrength
				}
			}
		}
	}

	g.applyWaterForces()
	g.applyGasForces()

	dragFactor := 1 - g.settings.airDrag
	bottomLimit := float32(screenHeight) - screenPadding
	rightLimit := float32(screenWidth)

	for i := range balls {
		if balls[i].material == MaterialStatic {
			continue
		}
		balls[i].velocity.vy += g.settings.gravity
		balls[i].velocity.vx *= dragFactor
		balls[i].velocity.vy *= dragFactor

		speedSq := balls[i].speedSquared()
		if speedSq > g.settings.maxSpeed*g.settings.maxSpeed {
			speed := float32(math.Sqrt(float64(speedSq)))
			scale := g.settings.maxSpeed / speed
			balls[i].velocity.vx *= scale
			balls[i].velocity.vy *= scale
		}

		balls[i].pos.x += balls[i].velocity.vx
		balls[i].pos.y += balls[i].velocity.vy

		// Top barrier (optional)
		if g.settings.hasTopBarrier {
			topLimit := screenPadding
			if balls[i].pos.y-balls[i].radius < topLimit {
				balls[i].pos.y = topLimit + balls[i].radius
				balls[i].velocity.vy *= -g.settings.groundRestitution
			}
		}

		if balls[i].pos.y+balls[i].radius > bottomLimit {
			balls[i].pos.y = bottomLimit - balls[i].radius
			balls[i].velocity.vy *= -g.settings.groundRestitution
			balls[i].velocity.vx *= g.settings.groundFriction
		}

		if balls[i].pos.x-balls[i].radius < 0 {
			balls[i].pos.x = balls[i].radius
			balls[i].velocity.vx *= -g.settings.groundRestitution
		}

		ballRightLimit := rightLimit - balls[i].radius
		if balls[i].pos.x > ballRightLimit {
			balls[i].pos.x = ballRightLimit
			balls[i].velocity.vx *= -g.settings.groundRestitution
		}
	}

	if len(balls) > 1 {
		for iteration := 0; iteration < maxCollisionSolves; iteration++ {
			g.collider.Clear()
			if len(g.cellCache) < len(balls) {
				g.cellCache = make([]cellCoord, len(balls))
			}
			for i := range balls {
				cx := g.collider.coord(balls[i].pos.x)
				cy := g.collider.coord(balls[i].pos.y)
				g.cellCache[i] = cellCoord{x: cx, y: cy}
				g.collider.insert(i, cx, cy)
			}

			anyResolved := false
			for i := range balls {
				coord := g.cellCache[i]
				for _, offset := range neighborOffsets {
					neighbors := g.collider.cell(coord.x+offset.dx, coord.y+offset.dy)
					for _, j := range neighbors {
						if j <= i {
							continue
						}
						a := &balls[i]
						b := &balls[j]
						ma := a.material
						mb := b.material
						switch {
						case ma == MaterialWater && mb == MaterialWater:
							continue
						case ma == MaterialGas && mb == MaterialGas:
							continue
						case (ma == MaterialWater && mb == MaterialGas) || (ma == MaterialGas && mb == MaterialWater):
							if resolveCollisionCustom(a, b, g.settings.collisionRestitution*0.2, 0.04) {
								anyResolved = true
							}
							continue
						case ma == MaterialWater || mb == MaterialWater:
							if resolveCollisionCustom(a, b, g.settings.collisionRestitution*0.25, 0.05) {
								anyResolved = true
							}
							continue
						case ma == MaterialGas || mb == MaterialGas:
							if resolveCollisionCustom(a, b, g.settings.collisionRestitution*0.3, 0.02) {
								anyResolved = true
							}
							continue
						default:
							if resolveCollision(a, b, g.settings.collisionRestitution) {
								anyResolved = true
							}
						}
					}
				}
			}
			if !anyResolved {
				break
			}
		}
	}

	return nil
}

func (g *Game) applyWaterForces() {
	if len(balls) == 0 {
		return
	}

	g.waterCollider.Clear()
	g.solidCollider.Clear()
	g.waterIndices = g.waterIndices[:0]
	g.solidIndices = g.solidIndices[:0]

	for i := range balls {
		switch balls[i].material {
		case MaterialWater:
			g.waterIndices = append(g.waterIndices, i)
		case MaterialSolid:
			g.solidIndices = append(g.solidIndices, i)
		case MaterialStatic:
			g.solidIndices = append(g.solidIndices, i)
		}
	}

	if len(g.waterIndices) == 0 {
		return
	}

	if len(g.waterCellCache) < len(g.waterIndices) {
		g.waterCellCache = make([]cellCoord, len(g.waterIndices))
	}
	if len(g.waterDensity) < len(g.waterIndices) {
		g.waterDensity = make([]float32, len(g.waterIndices))
	}
	if len(g.waterNearDensity) < len(g.waterIndices) {
		g.waterNearDensity = make([]float32, len(g.waterIndices))
	}

	for key := range g.waterIndexMap {
		delete(g.waterIndexMap, key)
	}

	for idx, ballIdx := range g.waterIndices {
		cx := g.waterCollider.coord(balls[ballIdx].pos.x)
		cy := g.waterCollider.coord(balls[ballIdx].pos.y)
		g.waterCellCache[idx] = cellCoord{x: cx, y: cy}
		g.waterCollider.insert(ballIdx, cx, cy)
		g.waterIndexMap[ballIdx] = idx
	}

	if len(g.solidIndices) > 0 {
		for _, ballIdx := range g.solidIndices {
			cx := g.solidCollider.coord(balls[ballIdx].pos.x)
			cy := g.solidCollider.coord(balls[ballIdx].pos.y)
			g.solidCollider.insert(ballIdx, cx, cy)
		}
	}

	interactionRadius := waterInteraction
	interactionRadiusSq := interactionRadius * interactionRadius

	for idx, ballIdx := range g.waterIndices {
		density := float32(0)
		nearDensity := float32(0)
		coord := g.waterCellCache[idx]
		for _, offset := range neighborOffsets {
			neighbors := g.waterCollider.cell(coord.x+offset.dx, coord.y+offset.dy)
			for _, neighborIdx := range neighbors {
				if neighborIdx == ballIdx {
					continue
				}
				if balls[neighborIdx].material != MaterialWater {
					continue
				}
				dx := balls[neighborIdx].pos.x - balls[ballIdx].pos.x
				dy := balls[neighborIdx].pos.y - balls[ballIdx].pos.y
				distSq := dx*dx + dy*dy
				if distSq >= interactionRadiusSq || distSq < minimumSeparation*minimumSeparation {
					continue
				}
				dist := float32(math.Sqrt(float64(distSq)))
				if dist <= 0 {
					continue
				}
				q := 1 - dist/interactionRadius
				density += q * q
				nearDensity += q * q * q
			}
		}
		g.waterDensity[idx] = density + 1
		g.waterNearDensity[idx] = nearDensity
	}

	for idx, ballIdx := range g.waterIndices {
		coord := g.waterCellCache[idx]
		density := g.waterDensity[idx]
		nearDensity := g.waterNearDensity[idx]
		pressure := waterPressureStiff * (density - waterRestDensity)
		nearPressure := waterNearStiff * nearDensity

		for _, offset := range neighborOffsets {
			neighbors := g.waterCollider.cell(coord.x+offset.dx, coord.y+offset.dy)
			for _, neighborIdx := range neighbors {
				if neighborIdx <= ballIdx {
					continue
				}
				neighborWaterIdx, ok := g.waterIndexMap[neighborIdx]
				if !ok {
					continue
				}

				dx := balls[neighborIdx].pos.x - balls[ballIdx].pos.x
				dy := balls[neighborIdx].pos.y - balls[ballIdx].pos.y
				distSq := dx*dx + dy*dy
				if distSq >= interactionRadiusSq || distSq < minimumSeparation*minimumSeparation {
					continue
				}
				dist := float32(math.Sqrt(float64(distSq)))
				if dist <= 0 {
					continue
				}
				q := 1 - dist/interactionRadius
				nx := dx / dist
				ny := dy / dist

				neighborDensity := g.waterDensity[neighborWaterIdx]
				neighborNearDensity := g.waterNearDensity[neighborWaterIdx]
				neighborPressure := waterPressureStiff * (neighborDensity - waterRestDensity)
				neighborNearPressure := waterNearStiff * neighborNearDensity

				pressureMag := (pressure + neighborPressure) * 0.5
				nearMag := (nearPressure + neighborNearPressure) * 0.5
				force := q*pressureMag + q*q*nearMag
				if force != 0 {
					impulseX := nx * force
					impulseY := ny * force
					balls[ballIdx].velocity.vx -= impulseX
					balls[ballIdx].velocity.vy -= impulseY
					balls[neighborIdx].velocity.vx += impulseX
					balls[neighborIdx].velocity.vy += impulseY
				}

				relVelX := balls[neighborIdx].velocity.vx - balls[ballIdx].velocity.vx
				relVelY := balls[neighborIdx].velocity.vy - balls[ballIdx].velocity.vy
				relAlongNormal := relVelX*nx + relVelY*ny
				viscImpulse := relAlongNormal * waterViscosity * q * 0.5
				viscX := nx * viscImpulse
				viscY := ny * viscImpulse
				balls[ballIdx].velocity.vx += viscX
				balls[ballIdx].velocity.vy += viscY
				balls[neighborIdx].velocity.vx -= viscX
				balls[neighborIdx].velocity.vy -= viscY
			}
		}
	}

	for idx, waterIdx := range g.waterIndices {
		waterBall := &balls[waterIdx]
		baseRange := waterBall.radius + waterRestDistance
		coord := g.waterCellCache[idx]
		for _, offset := range neighborOffsets {
			neighbors := g.solidCollider.cell(coord.x+offset.dx, coord.y+offset.dy)
			for _, solidIdx := range neighbors {
				dx := waterBall.pos.x - balls[solidIdx].pos.x
				dy := waterBall.pos.y - balls[solidIdx].pos.y
				allowed := balls[solidIdx].radius + baseRange
				distSq := dx*dx + dy*dy
				if distSq >= allowed*allowed || distSq < minimumSeparation*minimumSeparation {
					continue
				}
				dist := float32(math.Sqrt(float64(distSq)))
				if dist <= 0 {
					continue
				}
				nx := dx / dist
				ny := dy / dist
				penetration := allowed - dist
				push := penetration * waterBoundaryPush
				waterBall.velocity.vx += nx * push
				waterBall.velocity.vy += ny * push
				if balls[solidIdx].material != MaterialStatic {
					balls[solidIdx].velocity.vx -= nx * push * 0.25
					balls[solidIdx].velocity.vy -= ny * push * 0.25
				}

				tx := -ny
				ty := nx
				relVelX := waterBall.velocity.vx - balls[solidIdx].velocity.vx
				relVelY := waterBall.velocity.vy - balls[solidIdx].velocity.vy
				relTangential := relVelX*tx + relVelY*ty
				drag := relTangential * waterBoundaryDrag
				waterBall.velocity.vx -= tx * drag
				waterBall.velocity.vy -= ty * drag
				if balls[solidIdx].material != MaterialStatic {
					balls[solidIdx].velocity.vx += tx * drag * 0.25
					balls[solidIdx].velocity.vy += ty * drag * 0.25
				}
			}
		}
	}
}

func (g *Game) applyGasForces() {
	g.gasCollider.Clear()
	g.gasIndices = g.gasIndices[:0]

	for i := range balls {
		if balls[i].material == MaterialGas {
			g.gasIndices = append(g.gasIndices, i)
		}
	}

	if len(g.gasIndices) == 0 {
		return
	}

	if len(g.gasCellCache) < len(g.gasIndices) {
		g.gasCellCache = make([]cellCoord, len(g.gasIndices))
	}

	for idx, ballIdx := range g.gasIndices {
		cx := g.gasCollider.coord(balls[ballIdx].pos.x)
		cy := g.gasCollider.coord(balls[ballIdx].pos.y)
		g.gasCellCache[idx] = cellCoord{x: cx, y: cy}
		g.gasCollider.insert(ballIdx, cx, cy)
	}

	g.solidCollider.Clear()
	g.solidIndices = g.solidIndices[:0]
	for i := range balls {
		if balls[i].material != MaterialSolid && balls[i].material != MaterialStatic {
			continue
		}
		g.solidIndices = append(g.solidIndices, i)
		cx := g.solidCollider.coord(balls[i].pos.x)
		cy := g.solidCollider.coord(balls[i].pos.y)
		g.solidCollider.insert(i, cx, cy)
	}

	interactionRadius := gasInteraction
	interactionRadiusSq := interactionRadius * interactionRadius
	dragFactorX := 1 - gasDrag
	dragFactorY := 1 - gasDrag*0.5

	for _, ballIdx := range g.gasIndices {
		balls[ballIdx].velocity.vy -= gasBuoyancy
		balls[ballIdx].velocity.vx *= dragFactorX
		balls[ballIdx].velocity.vy *= dragFactorY
	}

	for idx, ballIdx := range g.gasIndices {
		coord := g.gasCellCache[idx]
		for _, offset := range neighborOffsets {
			neighbors := g.gasCollider.cell(coord.x+offset.dx, coord.y+offset.dy)
			for _, neighborIdx := range neighbors {
				if neighborIdx <= ballIdx {
					continue
				}
				dx := balls[neighborIdx].pos.x - balls[ballIdx].pos.x
				dy := balls[neighborIdx].pos.y - balls[ballIdx].pos.y
				distSq := dx*dx + dy*dy
				if distSq >= interactionRadiusSq || distSq < minimumSeparation*minimumSeparation {
					continue
				}
				dist := float32(math.Sqrt(float64(distSq)))
				if dist <= 0 {
					continue
				}
				nx := dx / dist
				ny := dy / dist
				q := 1 - dist/interactionRadius
				pressure := gasPressure * q * q
				impulseX := nx * pressure
				impulseY := ny * pressure
				balls[ballIdx].velocity.vx -= impulseX
				balls[ballIdx].velocity.vy -= impulseY
				balls[neighborIdx].velocity.vx += impulseX
				balls[neighborIdx].velocity.vy += impulseY

				relVelX := balls[neighborIdx].velocity.vx - balls[ballIdx].velocity.vx
				relVelY := balls[neighborIdx].velocity.vy - balls[ballIdx].velocity.vy
				relAlongNormal := relVelX*nx + relVelY*ny
				viscImpulse := relAlongNormal * gasViscosity * q * 0.5
				viscX := nx * viscImpulse
				viscY := ny * viscImpulse
				balls[ballIdx].velocity.vx += viscX
				balls[ballIdx].velocity.vy += viscY
				balls[neighborIdx].velocity.vx -= viscX
				balls[neighborIdx].velocity.vy -= viscY
			}
		}
	}

	if len(g.solidIndices) == 0 {
		return
	}

	for idx, gasIdx := range g.gasIndices {
		gasBall := &balls[gasIdx]
		baseRange := gasBall.radius + gasRestDistance
		coord := g.gasCellCache[idx]
		for _, offset := range neighborOffsets {
			neighbors := g.solidCollider.cell(coord.x+offset.dx, coord.y+offset.dy)
			for _, solidIdx := range neighbors {
				dx := gasBall.pos.x - balls[solidIdx].pos.x
				dy := gasBall.pos.y - balls[solidIdx].pos.y
				allowed := balls[solidIdx].radius + baseRange
				distSq := dx*dx + dy*dy
				if distSq >= allowed*allowed || distSq < minimumSeparation*minimumSeparation {
					continue
				}
				dist := float32(math.Sqrt(float64(distSq)))
				if dist <= 0 {
					continue
				}
				nx := dx / dist
				ny := dy / dist
				penetration := allowed - dist
				push := penetration * gasBoundaryPush
				gasBall.velocity.vx += nx * push
				gasBall.velocity.vy += ny * push
				if balls[solidIdx].material != MaterialStatic {
					balls[solidIdx].velocity.vx -= nx * push * 0.15
					balls[solidIdx].velocity.vy -= ny * push * 0.15
				}

				tx := -ny
				ty := nx
				relVelX := gasBall.velocity.vx - balls[solidIdx].velocity.vx
				relVelY := gasBall.velocity.vy - balls[solidIdx].velocity.vy
				relTangential := relVelX*tx + relVelY*ty
				drag := relTangential * gasBoundaryDrag
				gasBall.velocity.vx -= tx * drag
				gasBall.velocity.vy -= ty * drag
				if balls[solidIdx].material != MaterialStatic {
					balls[solidIdx].velocity.vx += tx * drag * 0.15
					balls[solidIdx].velocity.vy += ty * drag * 0.15
				}
			}
		}
	}
}

func (g *Game) Draw(screen *ebiten.Image) {
	fps := ebiten.CurrentFPS()
	shapeNames := []string{"Circle", "Square", "Triangle", "Water", "Gas", "Static"}
	shapeLabel := "Unknown"
	if int(currentShape) < len(shapeNames) {
		shapeLabel = shapeNames[currentShape]
	}
	bc := fmt.Sprintf("%.f particles | FPS: %.2f | ball radius: %.2f | attract radius: %.f | spawn count: %d | Shape: %s (1/2/3/4/5/6)",
		float64(len(balls)), fps, ballsize, moveAttractDistance, g.spawnClusterCount, shapeLabel)
	ebitenutil.DebugPrint(screen, bc)

	for i := range balls {
		var col color.Color
		switch balls[i].material {
		case MaterialWater:
			col = color.RGBA{R: 45, G: 134, B: 255, A: 200}
		case MaterialGas:
			col = color.RGBA{R: 220, G: 220, B: 255, A: 140}
		case MaterialStatic:
			col = color.RGBA{R: 180, G: 180, B: 195, A: 240}
		default:
			speed := balls[i].speed()
			col = velocityToColor(speed, g.settings.maxSpeed)
		}
		drawShape(screen, balls[i].shape, balls[i].pos.x, balls[i].pos.y, balls[i].radius, col)
	}

	if g.showMenu {
		// Draw semi-transparent overlay
		overlayColor := color.RGBA{R: 0, G: 0, B: 0, A: 180}
		vector.DrawFilledRect(screen, 0, 0, float32(screenWidth), float32(screenHeight), overlayColor, false)

		// Menu title
		menuX := float32(screenWidth)/2 - 200
		menuY := float32(screenHeight)/2 - 250
		title := "=== SETTINGS MENU ==="
		ebitenutil.DebugPrintAt(screen, title, int(menuX), int(menuY))

		menuY += 40
		ebitenutil.DebugPrintAt(screen, "Use UP/DOWN arrows to navigate", int(menuX), int(menuY))
		menuY += 15
		ebitenutil.DebugPrintAt(screen, "Use MOUSE WHEEL to adjust values", int(menuX), int(menuY))
		menuY += 15
		ebitenutil.DebugPrintAt(screen, "Hold SHIFT for faster changes", int(menuX), int(menuY))
		menuY += 15
		ebitenutil.DebugPrintAt(screen, "Press ESC to close menu", int(menuX), int(menuY))
		menuY += 40

		// Menu options
		options := []string{
			fmt.Sprintf("Gravity: %.2f", g.settings.gravity),
			fmt.Sprintf("Max Speed: %.2f", g.settings.maxSpeed),
			fmt.Sprintf("Move Away Distance: %.1f", g.settings.moveAwayDistance),
			fmt.Sprintf("Move Away Strength: %.2f", g.settings.moveAwayStrength),
			fmt.Sprintf("Move Attract Strength: %.2f", g.settings.moveAttractStrength),
			fmt.Sprintf("Ground Restitution: %.2f", g.settings.groundRestitution),
			fmt.Sprintf("Collision Restitution: %.2f", g.settings.collisionRestitution),
			fmt.Sprintf("Air Drag: %.3f", g.settings.airDrag),
			fmt.Sprintf("Ground Friction: %.2f", g.settings.groundFriction),
			fmt.Sprintf("Spawn Count: %d", g.spawnClusterCount),
			fmt.Sprintf("Top Barrier: %v", g.settings.hasTopBarrier),
			"EXIT GAME",
		}

		for i, option := range options {
			prefix := "  "
			if i == g.selectedOption {
				prefix = "> "
			}
			ebitenutil.DebugPrintAt(screen, prefix+option, int(menuX), int(menuY)+i*20)
		}
	}

	// Draw update button in top-right corner
	if !g.showMenu {
		buttonWidth := float32(140)
		buttonHeight := float32(30)
		buttonX := float32(screenWidth) - buttonWidth - 10
		buttonY := float32(10)

		// Check if mouse is hovering over button
		mx, my := ebiten.CursorPosition()
		g.updateButtonHover = float32(mx) >= buttonX && float32(mx) <= buttonX+buttonWidth &&
			float32(my) >= buttonY && float32(my) <= buttonY+buttonHeight

		// Draw button background
		buttonColor := color.RGBA{60, 60, 80, 200}
		if g.updateButtonHover {
			buttonColor = color.RGBA{80, 80, 120, 220}
		}
		if g.updateAvailable {
			buttonColor = color.RGBA{40, 120, 40, 200}
			if g.updateButtonHover {
				buttonColor = color.RGBA{60, 150, 60, 220}
			}
		}
		vector.DrawFilledRect(screen, buttonX, buttonY, buttonWidth, buttonHeight, buttonColor, false)

		// Draw button border
		borderColor := color.RGBA{150, 150, 180, 255}
		if g.updateButtonHover {
			borderColor = color.RGBA{200, 200, 230, 255}
		}
		vector.StrokeRect(screen, buttonX, buttonY, buttonWidth, buttonHeight, 2, borderColor, false)

		// Draw button text
		buttonText := "Check Updates"
		if g.updateChecking {
			buttonText = "Checking..."
		} else if g.updateAvailable {
			buttonText = "Update Available!"
		}
		ebitenutil.DebugPrintAt(screen, buttonText, int(buttonX+8), int(buttonY+10))

		// Show update message if available
		if g.updateMessage != "" {
			msgX := buttonX - 150
			msgY := buttonY + buttonHeight + 5
			msgWidth := float32(290)
			msgHeight := float32(30)

			// Message background
			vector.DrawFilledRect(screen, msgX, msgY, msgWidth, msgHeight, color.RGBA{40, 40, 50, 220}, false)
			ebitenutil.DebugPrintAt(screen, g.updateMessage, int(msgX+5), int(msgY+10))
		}
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return outsideWidth, outsideHeight
}

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// checkForUpdates checks if a newer version is available on GitHub
func checkForUpdates() (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", githubOwner, githubRepo)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}

	if release.TagName == version {
		return nil, nil // No update available
	}

	return &release, nil
}

// downloadFile downloads a file from a URL
func downloadFile(url, filepath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// extractZip extracts a zip file to a destination directory
func extractZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}

// selfUpdate downloads and installs the latest version
func selfUpdate() error {
	fmt.Println("Checking for updates...")
	release, err := checkForUpdates()
	if err != nil {
		return err
	}

	if release == nil {
		fmt.Println("You are already running the latest version:", version)
		return nil
	}

	fmt.Printf("New version available: %s (current: %s)\n", release.TagName, version)

	// Determine the correct asset based on OS and architecture
	osName := runtime.GOOS
	arch := runtime.GOARCH
	var assetName string
	
	switch osName {
	case "windows":
		assetName = fmt.Sprintf("phixgo-%s-windows-%s.zip", release.TagName, arch)
	case "darwin":
		assetName = fmt.Sprintf("phixgo-%s-darwin-%s.zip", release.TagName, arch)
	case "linux":
		assetName = fmt.Sprintf("phixgo-%s-linux-%s.zip", release.TagName, arch)
	default:
		return fmt.Errorf("unsupported operating system: %s", osName)
	}

	// Find the asset
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no compatible release found for %s-%s", osName, arch)
	}

	fmt.Printf("Downloading %s...\n", assetName)

	// Download to temporary file
	tmpFile := filepath.Join(os.TempDir(), assetName)
	if err := downloadFile(downloadURL, tmpFile); err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer os.Remove(tmpFile)

	fmt.Println("Extracting update...")

	// Extract to temporary directory
	tmpDir := filepath.Join(os.TempDir(), "phixgo-update")
	os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractZip(tmpFile, tmpDir); err != nil {
		return fmt.Errorf("failed to extract update: %w", err)
	}

	// Find the executable in the extracted files
	exeName := "phixgo"
	if osName == "windows" {
		exeName = "phixgo.exe"
	}

	var newExePath string
	err = filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), exeName) {
			newExePath = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return err
	}

	if newExePath == "" {
		return fmt.Errorf("executable not found in downloaded archive")
	}

	// Get current executable path
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current executable path: %w", err)
	}

	// Backup current executable
	backupPath := currentExe + ".old"
	if err := os.Rename(currentExe, backupPath); err != nil {
		return fmt.Errorf("failed to backup current executable: %w", err)
	}

	// Copy new executable
	newExe, err := os.Open(newExePath)
	if err != nil {
		os.Rename(backupPath, currentExe) // Restore backup on error
		return fmt.Errorf("failed to open new executable: %w", err)
	}
	defer newExe.Close()

	currentExeFile, err := os.OpenFile(currentExe, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		os.Rename(backupPath, currentExe) // Restore backup on error
		return fmt.Errorf("failed to create new executable: %w", err)
	}
	defer currentExeFile.Close()

	if _, err := io.Copy(currentExeFile, newExe); err != nil {
		os.Rename(backupPath, currentExe) // Restore backup on error
		return fmt.Errorf("failed to copy new executable: %w", err)
	}

	// Remove backup on success
	os.Remove(backupPath)

	fmt.Printf("Successfully updated to version %s!\n", release.TagName)
	fmt.Println("Please restart the application.")
	return nil
}

func main() {
	updateFlag := flag.Bool("update", false, "Check for updates and install the latest version")
	flag.Parse()

	if *updateFlag {
		if err := selfUpdate(); err != nil {
			fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	ebiten.SetWindowResizingMode(2)
	ebiten.SetFullscreen(true)
	ebiten.SetWindowTitle("PHIX")

	// Initialize empty image for triangle drawing
	emptyImage.Fill(color.White)

	fmt.Println(screenHeight, screenWidth)
	if err := ebiten.RunGame(NewGame()); err != nil {
		log.Fatal(err)
	}
}
