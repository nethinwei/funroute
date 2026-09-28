package machine

// kernelOp is the operation a kernel function's call lowers to: of one
// operand or two, and two the other way round for gt and ge.
type kernelOp struct {
	op    rop
	unary bool
	swap  bool
}

// kernelOps is, by signature, the kernel functions with an operation of
// their own. Only a kernel function lowers by it: a host that registers a
// function of the same signature on a registry without the kernel keeps its
// own. TestKernelOpsAreTheKernels holds every row to a kernel function.
var kernelOps = map[string]kernelOp{
	"add(int,int)->int":            {op: rAddI},
	"sub(int,int)->int":            {op: rSubI},
	"mul(int,int)->int":            {op: rMulI},
	"div(int,int)->int":            {op: rDivI},
	"mod(int,int)->int":            {op: rModI},
	"add(float,float)->float":      {op: rAddF},
	"sub(float,float)->float":      {op: rSubF},
	"mul(float,float)->float":      {op: rMulF},
	"div(float,float)->float":      {op: rDivF},
	"add(string,string)->string":   {op: rConcat},
	"lt(int,int)->bool":            {op: rLtI},
	"le(int,int)->bool":            {op: rLeI},
	"gt(int,int)->bool":            {op: rLtI, swap: true},
	"ge(int,int)->bool":            {op: rLeI, swap: true},
	"lt(float,float)->bool":        {op: rLtF},
	"le(float,float)->bool":        {op: rLeF},
	"gt(float,float)->bool":        {op: rLtF, swap: true},
	"ge(float,float)->bool":        {op: rLeF, swap: true},
	"lt(string,string)->bool":      {op: rLtS},
	"le(string,string)->bool":      {op: rLeS},
	"gt(string,string)->bool":      {op: rLtS, swap: true},
	"ge(string,string)->bool":      {op: rLeS, swap: true},
	"eq(T,T)->bool":                {op: rEq},
	"len(array<T>)->int":           {op: rLen, unary: true},
	"len(dict<T>)->int":            {op: rLen, unary: true},
	"at(array<T>,int)->T":          {op: rAt},
	"at(dict<T>,string)->T":        {op: rAtD},
	"take(array<T>,int)->array<T>": {op: rTake},
	"member(string,dict<T>)->bool": {op: rHasKey},
	"float(int)->float":            {op: rIntToF, unary: true},
	"int(float)->int":              {op: rFloatToI, unary: true},
	"int(int)->int":                {op: rMoveI, unary: true},
	"float(float)->float":          {op: rMoveF, unary: true},
}
