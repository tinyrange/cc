package ui

// Axis represents the main axis direction.
type Axis int

const (
	AxisHorizontal Axis = iota
	AxisVertical
)

// MainAxisAlignment controls distribution along the main axis.
type MainAxisAlignment int

const (
	MainAxisStart MainAxisAlignment = iota
	MainAxisCenter
	MainAxisEnd
	MainAxisSpaceBetween
	MainAxisSpaceAround
	MainAxisSpaceEvenly
)

// CrossAxisAlignment controls alignment on the cross axis.
type CrossAxisAlignment int

const (
	CrossAxisStart CrossAxisAlignment = iota
	CrossAxisCenter
	CrossAxisEnd
	CrossAxisStretch
)

// EdgeInsets represents padding/margin on all four sides.
type EdgeInsets struct {
	Left, Top, Right, Bottom float32
}

// All creates EdgeInsets with the same value on all sides.
func All(v float32) EdgeInsets {
	return EdgeInsets{Left: v, Top: v, Right: v, Bottom: v}
}

// Symmetric creates EdgeInsets with symmetric horizontal/vertical values.
func Symmetric(horizontal, vertical float32) EdgeInsets {
	return EdgeInsets{Left: horizontal, Top: vertical, Right: horizontal, Bottom: vertical}
}

// Only creates EdgeInsets with specific side values.
func Only(left, top, right, bottom float32) EdgeInsets {
	return EdgeInsets{Left: left, Top: top, Right: right, Bottom: bottom}
}

// Horizontal returns the total horizontal inset.
func (e EdgeInsets) Horizontal() float32 {
	return e.Left + e.Right
}

// Vertical returns the total vertical inset.
func (e EdgeInsets) Vertical() float32 {
	return e.Top + e.Bottom
}

// FlexLayoutParams define how a child participates in flex layout.
type FlexLayoutParams struct {
	// Flex is the flex grow factor (0 = no grow).
	Flex float32

	// Alignment overrides the container's cross-axis alignment for this child.
	Alignment *CrossAxisAlignment

	// Margin around this child.
	Margin EdgeInsets
}

// DefaultFlexParams returns default flex layout parameters.
func DefaultFlexParams() FlexLayoutParams {
	return FlexLayoutParams{Flex: 0}
}

// FlexParams returns flex layout parameters with a specific flex factor.
func FlexParams(flex float32) FlexLayoutParams {
	return FlexLayoutParams{Flex: flex}
}

// FlexParamsWithMargin returns flex params with margin.
func FlexParamsWithMargin(flex float32, margin EdgeInsets) FlexLayoutParams {
	return FlexLayoutParams{Flex: flex, Margin: margin}
}
