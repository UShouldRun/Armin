// The types of oplog.go.

package know

// opCode is the first byte of a log record.
type opCode uint8

const (
  opNode       opCode = iota + 1 // id parent kc flags born seen author name claim
  opClaim                        // id claim
  opMeta                         // id kc flags seen
  opMove                         // id parent name
  opEdge                         // from tgt
  opUnedge                       // from tgt
  opEvidence                     // node kind hash ref
  opReanchor                     // evidence hash ref
  opUnevidence                   // evidence
  opAuthor                       // role delegation model session
)

// decoder reads one op's payload: uvarints and length-prefixed
// strings. The first failure sticks in err.
type decoder struct {
  p   []byte
  err error
}
