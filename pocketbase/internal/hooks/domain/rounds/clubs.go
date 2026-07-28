package rounds

// DefaultClubs is the club pad, ported from rounds/clubs.py.
//
// It is a fixed list rather than a collection because it is one in Django too:
// a shot stores whatever club string it was given, and this is only what the
// play page offers. Making it per-player is issue #50, not this migration.
var DefaultClubs = []string{
	"Driver", "3W", "5i", "6i", "7i", "8i", "9i", "PW", "GW", "SW", "Putter",
}
