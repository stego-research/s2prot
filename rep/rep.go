/*

The Rep type that models a replay (and everything in it).

*/

package rep

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/stego-research/mpq"
	"github.com/stego-research/s2prot/v2"
	"github.com/stego-research/s2prot/v2/build"
)

const (
	// ParserVersion is a Semver2 compatible version of the parser.
	ParserVersion = "v2.0.0"
)

var (
	// ErrInvalidRepFile means invalid replay file.
	ErrInvalidRepFile = errors.New("Invalid SC2Replay file")

	// ErrUnsupportedRepVersion means the replay file is valid but its version is not supported.
	ErrUnsupportedRepVersion = errors.New("Unsupported replay version")

	// ErrDecoding means decoding the replay file failed,
	// Most likely because replay file is invalid, but also might be due to an implementation bug
	ErrDecoding = errors.New("Decoding error")
)

// Rep describes a replay.
type Rep struct {
	m *mpq.MPQ // MPQ parser for reading the file

	protocol *s2prot.Protocol // Protocol to decode the replay

	Header           Header           // Replay header (replay game version and length)
	Details          Details          // Game details (overall replay details)
	InitData         InitData         // Replay init data (the initial lobby)
	AttributesEvents AttributesEvents // Attributes events

	Metadata Metadata // Game metadata (calculated, confirmed results)

	GameEvents    []s2prot.Event // Game events
	MessageEvents []s2prot.Event // Message events
	TrackerEvents *TrackerEvents // Tracker events

	GameEventsErr    bool // Tells if decoding game events had errors
	MessageEventsErr bool // Tells if decoding message events had errors
	TrackerEventsErr bool // Tells if decoding tracker events had errors
}

// NewFromFile returns a new Rep constructed from a file.
// All types of events are decoded from the replay.
// The returned Rep must be closed with the Close method!
//
// ErrInvalidRepFile is returned if the specified name does not denote a valid SC2Replay file.
//
// ErrUnsupportedRepVersion is returned if the file exists and is a valid SC2Replay file but its version is not supported.
//
// ErrDecoding is returned if decoding the replay fails. This is most likely because the replay file is invalid, but also might be due to an implementation bug.
func NewFromFile(name string) (*Rep, error) {
	return NewFromFileEvents(name, true, true, true)
}

// NewFromFileEvents returns a new Rep constructed from a file, only the specified types of events decoded.
// The game, message and tracker tells if game events, message events and tracker events are to be decoded.
// Replay header, init data, details, attributes events and game metadata are always decoded.
// The returned Rep must be closed with the Close method!
//
// ErrInvalidRepFile is returned if the specified name does not denote a valid SC2Replay file.
//
// ErrUnsupportedRepVersion is returned if the file exists and is a valid SC2Replay file but its version is not supported.
//
// ErrDecoding is returned if decoding the replay fails. This is most likely because the replay file is invalid, but also might be due to an implementation bug.
func NewFromFileEvents(name string, game, message, tracker bool) (*Rep, error) {
	m, err := mpq.NewFromFile(name)
	if err != nil {
		return nil, ErrInvalidRepFile
	}
	return newRep(m, game, message, tracker)
}

// New returns a new Rep using the specified io.ReadSeeker as the SC2Replay file source.
// All types of events are decoded from the replay.
// The returned Rep must be closed with the Close method!
//
// ErrInvalidRepFile is returned if the input is not a valid SC2Replay file content.
//
// ErrUnsupportedRepVersion is returned if the input is a valid SC2Replay file but its version is not supported.
//
// ErrDecoding is returned if decoding the replay fails. This is most likely because the input is invalid, but also might be due to an implementation bug.
func New(input io.ReadSeeker) (*Rep, error) {
	return NewEvents(input, true, true, true)
}

// NewEvents returns a new Rep using the specified io.ReadSeeker as the SC2Replay file source, only the specified types of events decoded.
// The game, message and tracker tells if game events, message events and tracker events are to be decoded.
// Replay header, init data, details, attributes events and game metadata are always decoded.
// The returned Rep must be closed with the Close method!
//
// ErrInvalidRepFile is returned if the input is not a valid SC2Replay file content.
//
// ErrUnsupportedRepVersion is returned if the input is a valid SC2Replay file but its version is not supported.
//
// ErrDecoding is returned if decoding the replay fails. This is most likely because the input is invalid, but also might be due to an implementation bug.
func NewEvents(input io.ReadSeeker, game, message, tracker bool) (*Rep, error) {
	m, err := mpq.New(input)
	if err != nil {
		return nil, ErrInvalidRepFile
	}
	return newRep(m, game, message, tracker)
}

// newRep returns a new Rep constructed using the specified mpq.MPQ handler of the SC2Replay file, only the specified types of events decoded.
// The game, message and tracker tells if game events, message events and tracker events are to be decoded.
// Replay header, init data, details, attributes events and game metadata are always decoded.
// The returned Rep must be closed with the Close method!
//
// ErrInvalidRepFile is returned if the specified name does not denote a valid SC2Replay file.
//
// ErrUnsupportedRepVersion is returned if the input is a valid SC2Replay file but its version is not supported.
//
// ErrDecoding is returned if decoding the replay fails. This is most likely because the input is invalid, but also might be due to an implementation bug.
func newRep(m *mpq.MPQ, game, message, tracker bool) (parsedRep *Rep, errRes error) {
	closeMPQ := true
	defer func() {
		// If returning due to an error, MPQ must be closed!
		if closeMPQ {
			m.Close()
		}

		// The input is completely untrusted and the decoding implementation omits error checks for efficiency:
		// Protect replay decoding:
		if r := recover(); r != nil {
			errRes = ErrDecoding
		}
	}()

	rep := Rep{m: m}

	rep.Header = Header{Struct: s2prot.DecodeHeader(m.UserData())}
	if rep.Header.Struct == nil {
		return nil, ErrInvalidRepFile
	}

	bb := rep.Header.BaseBuild()
	p := s2prot.GetProtocol(int(bb))
	if p == nil {
		return nil, ErrUnsupportedRepVersion
	}
	rep.protocol = p

	data, err := m.FileByHash(620083690, 3548627612, 4013960850) // "replay.details"
	if err != nil || len(data) == 0 {
		// Attempt to open the anonymized version
		data, err = m.FileByHash(1421087648, 3590964654, 3400061273) // "replay.details.backup"
		if err != nil || len(data) == 0 {
			return nil, ErrInvalidRepFile
		}
	}
	rep.Details = Details{Struct: p.DecodeDetails(data)}

	data, err = m.FileByHash(3544165653, 1518242780, 4280631132) // "replay.initData"
	if err != nil || len(data) == 0 {
		// Attempt to open the anonymized version
		data, err = m.FileByHash(868899905, 1282002788, 1614930827) // "replay.initData.backup"
		if err != nil || len(data) == 0 {
			return nil, ErrInvalidRepFile
		}
	}
	rep.InitData = NewInitData(p.DecodeInitData(data))

	data, err = m.FileByHash(1306016990, 497594575, 2731474728) // "replay.attributes.events"
	if err != nil {
		return nil, ErrInvalidRepFile
	}
	rep.AttributesEvents = NewAttributesEvents(p.DecodeAttributesEvents(data))

	data, err = m.FileByHash(3675439372, 3912155403, 1108615308) // "replay.gamemetadata.json"
	if err != nil {
		return nil, ErrInvalidRepFile
	}
	if data != nil { // Might not be present, was added around 3.7
		if err = json.Unmarshal(data, &rep.Metadata.Struct); err != nil {
			return nil, ErrInvalidRepFile
		}
	}

	if game {
		data, err = m.FileByHash(496563520, 2864883019, 4101385109) // "replay.game.events"
		if err != nil {
			return nil, ErrInvalidRepFile
		}
		rep.GameEvents, err = p.DecodeGameEvents(data)
		rep.GameEventsErr = err != nil
	}

	if message {
		data, err = m.FileByHash(1089231967, 831857289, 1784674979) // "replay.message.events"
		if err != nil {
			return nil, ErrInvalidRepFile
		}
		rep.MessageEvents, err = p.DecodeMessageEvents(data)
		rep.MessageEventsErr = err != nil
	}

	if tracker {
		data, err = m.FileByHash(1501940595, 4263103390, 1648390237) // "replay.tracker.events"
		if err != nil {
			return nil, ErrInvalidRepFile
		}
		evts, err := p.DecodeTrackerEvents(data)
		rep.TrackerEvents = &TrackerEvents{Events: evts}
		rep.TrackerEvents.init(&rep)
		rep.TrackerEventsErr = err != nil
	}

	// Everything went well, Rep is about to be returned, do not close MPQ
	// (it will be the caller's responsibility, done via Rep.Close()).
	closeMPQ = false

	return &rep, nil
}

// findClosestSupportedBaseBuild finds the closest supported base build to the given base build.
// It considers both original Builds and Duplicates as supported builds.
// Returns the closest base build and true if any supported builds exist; otherwise 0 and false.
func findClosestSupportedBaseBuild(baseBuild int) (int, bool) {
	closest := 0
	bestDiff := int(^uint(0) >> 1) // max int
	found := false

	consider := func(b int) {
		d := b - baseBuild
		if d < 0 {
			d = -d
		}
		if d < bestDiff || (d == bestDiff && b < closest) {
			closest = b
			bestDiff = d
			found = true
		}
	}

	for b := range build.Builds {
		consider(b)
	}
	for b := range build.Duplicates {
		consider(b)
	}

	return closest, found
}

// NewEventsWithBuildCoercion returns a new Rep using the specified io.ReadSeeker as the SC2Replay file source,
// only the specified types of events decoded, but will coerce to the closest supported build protocol if
// the exact protocol for the replay's base build is not available.
// The returned int is the coerced base build used for parsing; it is 0 if no coercion was needed.
//
// ErrInvalidRepFile is returned if the input is not a valid SC2Replay file content.
//
// ErrUnsupportedRepVersion is returned if the input is a valid SC2Replay file but no supported protocol exists to coerce to.
//
// ErrDecoding is returned if decoding the replay fails. This is most likely because the input is invalid, but also might be due to an implementation bug.
func NewEventsWithBuildCoercion(input io.ReadSeeker, game, message, tracker bool) (*Rep, int, error) {
	m, err := mpq.New(input)
	if err != nil {
		return nil, 0, ErrInvalidRepFile
	}
	rep, coercedTo, err := newRepWithBuildCoercion(m, game, message, tracker)
	return rep, coercedTo, err
}

// NewFromFileEventsWithBuildCoercion returns a new Rep constructed from a file, only the specified types of events decoded,
// but will coerce to the closest supported build protocol if the exact protocol for the replay's base build is not available.
// The returned int is the coerced base build used for parsing; it is 0 if no coercion was needed.
//
// ErrInvalidRepFile is returned if the specified name does not denote a valid SC2Replay file.
//
// ErrUnsupportedRepVersion is returned if the file exists and is a valid SC2Replay file but no supported protocol exists to coerce to.
//
// ErrDecoding is returned if decoding the replay fails. This is most likely because the replay file is invalid, but also might be due to an implementation bug.
func NewFromFileEventsWithBuildCoercion(name string, game, message, tracker bool) (*Rep, int, error) {
	m, err := mpq.NewFromFile(name)
	if err != nil {
		return nil, 0, ErrInvalidRepFile
	}
	rep, coercedTo, err := newRepWithBuildCoercion(m, game, message, tracker)
	return rep, coercedTo, err
}

// newRepWithBuildCoercion returns a new Rep constructed using the specified mpq.MPQ handler of the SC2Replay file,
// only the specified types of events decoded. It behaves like newRep, but if the replay's base build does not have a
// matching protocol, it will choose the closest supported build (by absolute difference) and attempt to parse using that.
// The returned int is the coerced base build used for parsing; it is 0 if no coercion was needed.
// The returned Rep must be closed with the Close method!
//
// ErrInvalidRepFile is returned if the specified input is not a valid SC2Replay file.
//
// ErrUnsupportedRepVersion is returned if there is no supported protocol to coerce to.
//
// ErrDecoding is returned if decoding the replay fails. This is most likely because the input is invalid, but also might be due to an implementation bug.
func newRepWithBuildCoercion(m *mpq.MPQ, game, message, tracker bool) (parsedRep *Rep, coercedTo int, errRes error) {
	closeMPQ := true
	defer func() {
		// If returning due to an error, MPQ must be closed!
		if closeMPQ {
			m.Close()
		}

		// The input is completely untrusted and the decoding implementation omits error checks for efficiency:
		// Protect replay decoding:
		if r := recover(); r != nil {
			errRes = ErrDecoding
		}
	}()

	rep := Rep{m: m}

	rep.Header = Header{Struct: s2prot.DecodeHeader(m.UserData())}
	if rep.Header.Struct == nil {
		return nil, 0, ErrInvalidRepFile
	}

	bb := int(rep.Header.BaseBuild())
	p := s2prot.GetProtocol(bb)
	if p == nil {
		// find closest supported build
		closest, ok := findClosestSupportedBaseBuild(bb)
		if !ok {
			return nil, 0, ErrUnsupportedRepVersion
		}
		p = s2prot.GetProtocol(closest)
		if p == nil {
			// Should not happen but guard anyway
			return nil, 0, ErrUnsupportedRepVersion
		}
		coercedTo = closest
	}
	rep.protocol = p

	data, err := m.FileByHash(620083690, 3548627612, 4013960850) // "replay.details"
	if err != nil || len(data) == 0 {
		// Attempt to open the anonymized version
		data, err = m.FileByHash(1421087648, 3590964654, 3400061273) // "replay.details.backup"
		if err != nil || len(data) == 0 {
			return nil, 0, ErrInvalidRepFile
		}
	}
	rep.Details = Details{Struct: p.DecodeDetails(data)}

	data, err = m.FileByHash(3544165653, 1518242780, 4280631132) // "replay.initData"
	if err != nil || len(data) == 0 {
		// Attempt to open the anonymized version
		data, err = m.FileByHash(868899905, 1282002788, 1614930827) // "replay.initData.backup"
		if err != nil || len(data) == 0 {
			return nil, 0, ErrInvalidRepFile
		}
	}
	rep.InitData = NewInitData(p.DecodeInitData(data))

	data, err = m.FileByHash(1306016990, 497594575, 2731474728) // "replay.attributes.events"
	if err != nil {
		return nil, 0, ErrInvalidRepFile
	}
	rep.AttributesEvents = NewAttributesEvents(p.DecodeAttributesEvents(data))

	data, err = m.FileByHash(3675439372, 3912155403, 1108615308) // "replay.gamemetadata.json"
	if err != nil {
		return nil, 0, ErrInvalidRepFile
	}
	if data != nil { // Might not be present, was added around 3.7
		if err = json.Unmarshal(data, &rep.Metadata.Struct); err != nil {
			return nil, 0, ErrInvalidRepFile
		}
	}

	if game {
		data, err = m.FileByHash(496563520, 2864883019, 4101385109) // "replay.game.events"
		if err != nil {
			return nil, 0, ErrInvalidRepFile
		}
		rep.GameEvents, err = p.DecodeGameEvents(data)
		rep.GameEventsErr = err != nil
	}

	if message {
		data, err = m.FileByHash(1089231967, 831857289, 1784674979) // "replay.message.events"
		if err != nil {
			return nil, 0, ErrInvalidRepFile
		}
		rep.MessageEvents, err = p.DecodeMessageEvents(data)
		rep.MessageEventsErr = err != nil
	}

	if tracker {
		data, err = m.FileByHash(1501940595, 4263103390, 1648390237) // "replay.tracker.events"
		if err != nil {
			return nil, 0, ErrInvalidRepFile
		}
		evts, err := p.DecodeTrackerEvents(data)
		rep.TrackerEvents = &TrackerEvents{Events: evts}
		rep.TrackerEvents.init(&rep)
		rep.TrackerEventsErr = err != nil
	}

	// Everything went well, Rep is about to be returned, do not close MPQ
	// (it will be the caller's responsibility, done via Rep.Close()).
	closeMPQ = false

	return &rep, coercedTo, nil
}

// Close closes the Rep and its resources.
func (r *Rep) Close() error {
	if r.m == nil {
		return nil
	}
	return r.m.Close()
}

// MPQ gives access to the underlying MPQ parser of the rep.
// Intentionally not a method of Rep to not urge its use.
func MPQ(r *Rep) *mpq.MPQ {
	return r.m
}
