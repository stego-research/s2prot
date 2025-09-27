/*

Type describing the attributes events.

*/

package rep

import "github.com/stego-research/s2prot/v2"

// Attribute ID constants
const (
	// attrGameMode is the game mode attribute
	attrGameMode = "3009"
)

// scopeGlobal is the global scope.
const scopeGlobal = "16"

// AttributesEvents contains game attributes.
type AttributesEvents struct {
	s2prot.Struct

	// Scopes
	scopes s2prot.Struct
}

// NewAttributesEvents creates a new attributes events from the specified Struct.
func NewAttributesEvents(s s2prot.Struct) AttributesEvents {
	a := AttributesEvents{
		Struct: s,
		scopes: s.Structv("scopes"),
	}
	return a
}

// Source returns the source.
func (a *AttributesEvents) Source() string {
	return a.Stringv("source")
}

// MapNamespace returns the map namespace.
func (a *AttributesEvents) MapNamespace() string {
	return a.Stringv("mapNamespace")
}

// GameMode returns the game mode
func (a *AttributesEvents) GameMode() *GameMode {
	if a.scopes == nil {
		return GameModeUnknown
	}
	return gameModeByAttrValue(a.scopes.Stringv(scopeGlobal, attrGameMode, "value"))
}
