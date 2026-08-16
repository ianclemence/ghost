package profiles

import (
	"fmt"
	"math"
)

type AvatarSVG struct {
	Shape string
	Color string
	Name  string
	Size  int
}

func (a *AvatarSVG) Render() string {
	size := a.Size
	if size == 0 {
		size = 64
	}
	half := size / 2
	r := float64(half - 2)

	switch a.Shape {
	case AvatarShapeCircle:
		return a.circleSVG(size, half, r)
	case AvatarShapeSquircle:
		return a.squircleSVG(size, half, r)
	case AvatarShapePill:
		return a.pillSVG(size, half, r)
	case AvatarShapeTriangle:
		return a.triangleSVG(size, half, r)
	case AvatarShapeHexagon:
		return a.hexagonSVG(size, half, r)
	case AvatarShapeCloud:
		return a.cloudSVG(size, half, r)
	case AvatarShapeDrop:
		return a.dropSVG(size, half, r)
	default:
		return a.circleSVG(size, half, r)
	}
}

func (a *AvatarSVG) circleSVG(size, half int, r float64) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <circle cx="%d" cy="%d" r="%.1f" fill="%s"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
</svg>`,
		size, size, size, size,
		half, half, r,
		a.Color,
		float64(half)-r*0.3, float64(half)-r*0.1,
		float64(half)+r*0.3, float64(half)-r*0.1,
	)
}

func (a *AvatarSVG) squircleSVG(size, half int, r float64) string {
	s := float64(size)
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect x="2" y="2" width="%d" height="%d" rx="%.1f" ry="%.1f" fill="%s"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
</svg>`,
		size, size, size, size,
		size-4, size-4, s*0.25, s*0.25,
		a.Color,
		float64(half)-r*0.3, float64(half)-r*0.1,
		float64(half)+r*0.3, float64(half)-r*0.1,
	)
}

func (a *AvatarSVG) pillSVG(size, half int, r float64) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect x="2" y="%d" width="%d" height="%d" rx="%d" fill="%s"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
</svg>`,
		size, size, size, size,
		half/2, size-4, half,
		half/2,
		a.Color,
		float64(half)-r*0.3, float64(half)-r*0.1,
		float64(half)+r*0.3, float64(half)-r*0.1,
	)
}

func (a *AvatarSVG) triangleSVG(size, half int, r float64) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <polygon points="%d,2 2,%d %d,%d" fill="%s"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
</svg>`,
		size, size, size, size,
		half, size-2, size-2, size-2,
		a.Color,
		float64(half)-r*0.3, float64(half)+r*0.1,
		float64(half)+r*0.3, float64(half)+r*0.1,
	)
}

func (a *AvatarSVG) hexagonSVG(size, half int, r float64) string {
	points := ""
	for i := 0; i < 6; i++ {
		angle := float64(i)*60 - 90
		x := float64(half) + r*math.Cos(angle*math.Pi/180)
		y := float64(half) + r*math.Sin(angle*math.Pi/180)
		if i > 0 {
			points += " "
		}
		points += fmt.Sprintf("%.1f,%.1f", x, y)
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <polygon points="%s" fill="%s"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
</svg>`,
		size, size, size, size,
		points, a.Color,
		float64(half)-r*0.3, float64(half)-r*0.1,
		float64(half)+r*0.3, float64(half)-r*0.1,
	)
}

func (a *AvatarSVG) cloudSVG(size, half int, r float64) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <ellipse cx="%.1f" cy="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>
  <ellipse cx="%.1f" cy="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>
  <ellipse cx="%.1f" cy="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
</svg>`,
		size, size, size, size,
		float64(half), float64(half)-r*0.1, r*0.9, r*0.5, a.Color,
		float64(half)-r*0.4, float64(half)+r*0.1, r*0.5, r*0.4, a.Color,
		float64(half)+r*0.4, float64(half)+r*0.1, r*0.5, r*0.4, a.Color,
		float64(half)-r*0.3, float64(half)-r*0.1,
		float64(half)+r*0.3, float64(half)-r*0.1,
	)
}

func (a *AvatarSVG) dropSVG(size, half int, r float64) string {
	cx := float64(half)
	cy := float64(half)
	dropTop := 2.0
	dropMid := cy - r*0.2
	dropBot := cy + r*0.7
	rx := r * 0.5
	ry := r * 0.6

	path := fmt.Sprintf("M%.1f,%.1f Q%.1f,%.1f %.1f,%.1f A%.1f,%.1f 0 1,1 %.1f,%.1f Q%.1f,%.1f %.1f,%.1f Z",
		cx, dropTop,
		cx, dropMid,
		cx+rx, dropBot,
		rx, ry,
		cx-rx, dropBot,
		cx, dropMid,
		cx, dropTop,
	)

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <path d="%s" fill="%s"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
  <circle cx="%.1f" cy="%.1f" r="2" fill="#1a1a1a"/>
</svg>`,
		size, size, size, size,
		path, a.Color,
		cx-r*0.25, cy+r*0.15,
		cx+r*0.25, cy+r*0.15,
	)
}

func DefaultAvatar(name string) *Avatar {
	avatars := []Avatar{
		{Shape: AvatarShapeCircle, Color: "#3b82f6"},
		{Shape: AvatarShapeSquircle, Color: "#8b5cf6"},
		{Shape: AvatarShapePill, Color: "#22c55e"},
		{Shape: AvatarShapeTriangle, Color: "#f97316"},
		{Shape: AvatarShapeHexagon, Color: "#ec4899"},
		{Shape: AvatarShapeCloud, Color: "#06b6d4"},
		{Shape: AvatarShapeDrop, Color: "#eab308"},
	}

	hash := 0
	for _, c := range name {
		hash = hash*31 + int(c)
	}
	idx := hash % len(avatars)

	return &avatars[idx]
}
