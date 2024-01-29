// Code generated by "stringer -type=poolerError -linecomment -output pooler_generated.go"; DO NOT EDIT.

package kitchen

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[nilPoolable-0]
	_ = x[nilCreatorFunc-1]
	_ = x[nilAfterGetFunc-2]
	_ = x[nilPoolerToRecycler-3]
	_ = x[poolerDisabled-4]
	_ = x[recyclerDisabled-5]
	_ = x[initpoolerError-6]
}

const _poolerError_name = "nil Poolablenil New function passed to CreatePoolernil AfterGet function passed to CreatePoolernil Pooler passed to CreateRecyclerPooler disabledRecycler disablederror creating Recycler"

var _poolerError_index = [...]uint8{0, 12, 51, 95, 130, 145, 162, 185}

func (i poolerError) String() string {
	if i < 0 || i >= poolerError(len(_poolerError_index)-1) {
		return "poolerError(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _poolerError_name[_poolerError_index[i]:_poolerError_index[i+1]]
}
