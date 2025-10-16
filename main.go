package main

import (
	"fmt"
	"image/color"
	"log"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	screenPadding     = float32(50.0)
	minimumSeparation = float32(0.0001)
	minSpawnRadius    = float32(4.0)
	maxSpawnRadius    = float32(120.0)
	ballSpawnStep     = 0.5
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
	settings         Settings
	showMenu         bool
	selectedOption   int
	prevEscPressed   bool
	prevUpPressed    bool
	prevDownPressed  bool
}

func NewGame() *Game {
	return &Game{
		settings: defaultSettings(),
		showMenu: false,
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
)

type Ball struct {
	pos      Pos
	velocity Velocity
	radius   float32
	shape    ShapeType
}

func createBall(pos Pos, r float32, shape ShapeType) Ball {
	return Ball{pos: pos, velocity: Velocity{vx: 0, vy: 0}, radius: r, shape: shape}
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

func resolveCollision(b1, b2 *Ball, collisionRestitution float32) {
	dx := b2.pos.x - b1.pos.x
	dy := b2.pos.y - b1.pos.y
	nx, ny, distance := normalize(dx, dy)
	if nx == 0 && ny == 0 {
		nx = 1
	}
	combinedRadius := b1.radius + b2.radius
	overlap := combinedRadius - distance
	if overlap <= 0 {
		return
	}

	// Push balls apart proportionally to their radii to avoid jitter.
	totalRadius := combinedRadius
	if totalRadius < minimumSeparation {
		totalRadius = minimumSeparation
	}
	shift1 := overlap * (b2.radius / totalRadius)
	shift2 := overlap * (b1.radius / totalRadius)
	b1.pos.x -= nx * shift1
	b1.pos.y -= ny * shift1
	b2.pos.x += nx * shift2
	b2.pos.y += ny * shift2

	// Relative velocity along the normal.
	rvx := b2.velocity.vx - b1.velocity.vx
	rvy := b2.velocity.vy - b1.velocity.vy
	velAlongNormal := rvx*nx + rvy*ny
	if velAlongNormal > 0 {
		return
	}

	restitution := collisionRestitution
	impulseScalar := -(1 + restitution) * velAlongNormal / 2 // equal mass assumption
	impulseX := impulseScalar * nx
	impulseY := impulseScalar * ny

	b1.velocity.vx -= impulseX
	b1.velocity.vy -= impulseY
	b2.velocity.vx += impulseX
	b2.velocity.vy += impulseY

	// Simple tangential friction to reduce sliding after collision.
	// Compute tangential speed and apply proportional friction impulse.
	tx := -ny
	ty := nx
	relTangential := rvx*tx + rvy*ty
	frictionImpulse := relTangential * 0.5
	b1.velocity.vx += tx * frictionImpulse
	b1.velocity.vy += ty * frictionImpulse
	b2.velocity.vx -= tx * frictionImpulse
	b2.velocity.vy -= ty * frictionImpulse
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
		path.MoveTo(x, y-height*0.67)                    // Top vertex
		path.LineTo(x-radius, y+height*0.33)             // Bottom left
		path.LineTo(x+radius, y+height*0.33)             // Bottom right
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
	}
}

var emptyImage = ebiten.NewImage(3, 3)

var (
	ballsize             float64 = 10
	moveAttractDistance  float64 = 200.0
	balls                []Ball
	ballSpawnTimer       int
	currentShape         ShapeType = ShapeCircle
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
				g.selectedOption = 10 // Number of menu options - 1
			}
		}
		if downPressed && !g.prevDownPressed {
			g.selectedOption++
			if g.selectedOption > 10 {
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
			case 9: // Top Barrier
				if my != 0 {
					g.settings.hasTopBarrier = !g.settings.hasTopBarrier
				}
			case 10: // Exit
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
			radiiOffsets := []float64{3, 0, -3}
			for _, offset := range radiiOffsets {
				size := ballsize + offset
				radius := float32(math.Min(math.Max(size, float64(minSpawnRadius)), float64(maxSpawnRadius)))
				balls = append(balls, createBall(createPos(float32(x), float32(y)), radius, currentShape))
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

	dragFactor := 1 - g.settings.airDrag
	bottomLimit := float32(screenHeight) - screenPadding
	rightLimit := float32(screenWidth)
	
	for i := range balls {
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

	for i := 0; i < len(balls); i++ {
		for j := i + 1; j < len(balls); j++ {
			resolveCollision(&balls[i], &balls[j], g.settings.collisionRestitution)
		}
	}

	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	fps := ebiten.CurrentFPS()
	shapeName := []string{"Circle", "Square", "Triangle"}[currentShape]
	bc := fmt.Sprintf("%.f balls | FPS: %.2f | ball radius: %.2f | attract radius: %.f | Shape: %s (1/2/3)", 
		float64(len(balls)), fps, ballsize, moveAttractDistance, shapeName)
	ebitenutil.DebugPrint(screen, bc)

	for i := range balls {
		speed := balls[i].speed()
		col := velocityToColor(speed, g.settings.maxSpeed)
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
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return outsideWidth, outsideHeight
}

func main() {
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
