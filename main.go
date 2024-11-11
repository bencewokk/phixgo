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
	gravity                = 0.09
	floorFriction          = 0.5
	maxSpeed               = 10.0
	velocityGrowthFactor   = 1.05
	velocityTransferFactor = 0.3
	moveAwayDistance       = 100.0
	moveAwayStrength       = 5.0
	moveAttractDistance    = 200.0
	moveAttractStrength    = 5.0
	screenPadding          = 50.0
)

var (
	screenWidth, screenHeight = ebiten.ScreenSizeInFullscreen()
)

type Game struct{}

type Pos struct {
	x, y float32
}

func createPos(x, y float32) Pos {
	return Pos{x: x, y: y}
}

type Velocity struct {
	vx, vy float32
}

type Ball struct {
	pos      Pos
	velocity Velocity
	radius   float32
}

func createBall(pos Pos, r float32) Ball {
	return Ball{pos: pos, velocity: Velocity{vx: 0, vy: 0}, radius: r}
}

func (b *Ball) speed() float32 {
	return float32(math.Sqrt(float64(b.velocity.vx*b.velocity.vx + b.velocity.vy*b.velocity.vy)))
}

func (b1 *Ball) collidesWith(b2 *Ball) bool {
	dx := b1.pos.x - b2.pos.x
	dy := b1.pos.y - b2.pos.y
	distance := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	return distance < (b1.radius + b2.radius)
}

func velocityToColor(velocity float32) color.Color {
	normalizedSpeed := velocity / maxSpeed
	if normalizedSpeed > 1 {
		normalizedSpeed = 1
	}

	g := uint8(normalizedSpeed * 255)
	b := uint8((1 - normalizedSpeed) * 255)

	return color.RGBA{R: 0, G: g, B: b, A: 255}
}

func (g *Game) Update() error {
	_, my := ebiten.Wheel()

	if my < 0 {
		d += 0.2
	} else if my > 0 {
		d -= 0.2
	}

	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		x, y := ebiten.CursorPosition()

		if ebiten.IsKeyPressed(ebiten.KeyShift) {
			for i := len(balls) - 1; i >= 0; i-- {
				dx := balls[i].pos.x - float32(x)
				dy := balls[i].pos.y - float32(y)
				distance := float32(math.Sqrt(float64(dx*dx + dy*dy)))

				if distance-15 < balls[i].radius {
					balls = append(balls[:i], balls[i+1:]...)
				}
			}
		} else if d != 0 {
			balls = append(balls, createBall(createPos(float32(x), float32(y)), float32(math.Abs(d))))
			balls = append(balls, createBall(createPos(float32(x), float32(y)), float32(math.Abs(d))))
		}
	}

	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonRight) {
		x, y := ebiten.CursorPosition()
		mousePos := createPos(float32(x), float32(y))

		if ebiten.IsKeyPressed(ebiten.KeyShift) {
			for i := range balls {
				dx := balls[i].pos.x - mousePos.x
				dy := balls[i].pos.y - mousePos.y
				distance := float32(math.Sqrt(float64(dx*dx + dy*dy)))

				if distance < moveAttractDistance {
					moveX := -dx / distance * moveAttractStrength
					moveY := -dy / distance * moveAttractStrength

					balls[i].velocity.vx += moveX
					balls[i].velocity.vy += moveY
				}
			}
		} else {
			for i := range balls {
				dx := balls[i].pos.x - mousePos.x
				dy := balls[i].pos.y - mousePos.y
				distance := float32(math.Sqrt(float64(dx*dx + dy*dy)))

				if distance < moveAwayDistance {
					moveX := dx / distance * moveAwayStrength
					moveY := dy / distance * moveAwayStrength

					balls[i].velocity.vx += moveX
					balls[i].velocity.vy += moveY
				}
			}
		}
	}

	for i := range balls {
		balls[i].velocity.vy += gravity
		balls[i].velocity.vy *= 0.99
		balls[i].velocity.vx *= 0.99

		balls[i].pos.x += balls[i].velocity.vx
		balls[i].pos.y += balls[i].velocity.vy

		if balls[i].pos.y+balls[i].radius > float32(screenHeight)-screenPadding {
			balls[i].pos.y = float32(screenHeight) - balls[i].radius - screenPadding
			balls[i].velocity.vy *= -0.5
		}

		if balls[i].pos.x-balls[i].radius < 0 {
			balls[i].pos.x = balls[i].radius
			balls[i].velocity.vx *= -1
		} else if balls[i].pos.x+balls[i].radius > float32(screenWidth) {
			balls[i].pos.x = float32(screenWidth) - balls[i].radius
			balls[i].velocity.vx *= -1
		}
	}

	for i := 0; i < len(balls); i++ {
		for j := i + 1; j < len(balls); j++ {
			if balls[i].collidesWith(&balls[j]) {
				transferX := (balls[j].velocity.vx - balls[i].velocity.vx) * velocityTransferFactor
				transferY := (balls[j].velocity.vy - balls[i].velocity.vy) * velocityTransferFactor

				balls[i].velocity.vx += transferX
				balls[i].velocity.vy += transferY
				balls[j].velocity.vx -= transferX
				balls[j].velocity.vy -= transferY

				overlap := (balls[i].radius + balls[j].radius) - float32(math.Sqrt(float64((balls[i].pos.x-balls[j].pos.x)*(balls[i].pos.x-balls[j].pos.x)+(balls[i].pos.y-balls[j].pos.y)*(balls[i].pos.y-balls[j].pos.y))))
				moveX := (balls[i].pos.x - balls[j].pos.x) * overlap / 80
				moveY := (balls[i].pos.y - balls[j].pos.y) * overlap / 120
				balls[i].pos.x += moveX
				balls[i].pos.y += moveY
				balls[j].pos.x -= moveX
				balls[j].pos.y -= moveY
			}
		}
	}

	return nil
}

var balls []Ball
var d float64

func (g *Game) Draw(screen *ebiten.Image) {
	fps := ebiten.CurrentFPS()
	bc := fmt.Sprintf("%.f balls | FPS: %.2f | ball radius: %.2f", float64(len(balls)), fps, math.Abs(d))
	ebitenutil.DebugPrint(screen, bc)

	for i := 0; i < len(balls); i++ {
		speed := balls[i].speed()
		color := velocityToColor(speed)
		vector.DrawFilledCircle(screen, balls[i].pos.x, balls[i].pos.y, balls[i].radius, color, false)
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return outsideWidth, outsideHeight
}

func main() {
	ebiten.SetWindowResizingMode(2)
	ebiten.SetFullscreen(true)
	ebiten.SetWindowTitle("PHIX")

	fmt.Println(screenHeight, screenWidth)
	if err := ebiten.RunGame(&Game{}); err != nil {
		log.Fatal(err)
	}
}
