package exec

import (
	"reflect"

	"github.com/qiniu/x/log"
)

func execGoFunc(i Instr, p *Context) {
	idx := i & bitsOperand
	gofuns[idx].exec(0, p)
}

func execGoFuncv(i Instr, p *Context) {
	idx := i & bitsOpCallFuncvOperand
	arity := (i >> bitsOpCallFuncvShift) & bitsFuncvArityOperand
	fun := gofunvs[idx]
	if arity == bitsFuncvArityVar {
		v := p.Pop()
		args := reflect.ValueOf(v)
		n := args.Len()
		for i := 0; i < n; i++ {
			p.Push(args.Index(i).Interface())
		}
		arity = uint32(fun.getNumIn() - 1 + n)
	} else if arity == bitsFuncvArityMax {
		arity = uint32(p.Pop().(int) + bitsFuncvArityMax)
	}
	fun.exec(arity, p)
}

// -----------------------------------------------------------------------------

// SymbolKind represents symbol kind.
type SymbolKind uint32

const (
	// SymbolFunc - function
	SymbolFunc SymbolKind = opCallGoFunc
	// SymbolFuncv - variadic function
	SymbolFuncv SymbolKind = opCallGoFuncv
	// SymbolVar - variable
	SymbolVar SymbolKind = 0
)

// GoPackage represents a Go package.
type GoPackage struct {
	PkgPath string
	syms    map[string]uint32
	types   map[string]reflect.Type
}

// NewGoPackage creates a new builtin Go Package.
func NewGoPackage(pkgPath string) *GoPackage {
	if _, ok := gopkgs[pkgPath]; ok {
		log.Panicln("NewPackage failed: package exists -", pkgPath)
	}
	pkg := &GoPackage{
		PkgPath: pkgPath,
		syms:    make(map[string]uint32),
		types:   make(map[string]reflect.Type),
	}
	gopkgs[pkgPath] = pkg
	return pkg
}

// FindGoPackage lookups a Go package by pkgPath. It returns nil if not found.
func FindGoPackage(pkgPath string) *GoPackage {
	return gopkgs[pkgPath]
}

// Find lookups a symbol by specified its name.
func (p *GoPackage) Find(name string) (addr uint32, kind SymbolKind, ok bool) {
	if p == nil {
		return
	}
	if v, ok := p.syms[name]; ok {
		return v & bitsOperand, SymbolKind(v >> bitsOpShift), true
	}
	return
}

// FindFunc lookups a Go function by name.
func (p *GoPackage) FindFunc(name string) (addr GoFuncAddr, ok bool) {
	if v, ok := p.syms[name]; ok {
		if (v >> bitsOpShift) == opCallGoFunc {
			return GoFuncAddr(v & bitsOperand), true
		}
	}
	return
}

// FindFuncv lookups a Go function by name.
func (p *GoPackage) FindFuncv(name string) (addr GoFuncvAddr, ok bool) {
	if v, ok := p.syms[name]; ok {
		if (v >> bitsOpShift) == opCallGoFuncv {
			return GoFuncvAddr(v & bitsOperand), true
		}
	}
	return
}

// FindVar lookups a Go variable by name.
func (p *GoPackage) FindVar(name string) (addr GoVarAddr, ok bool) {
	if v, ok := p.syms[name]; ok {
		if (v >> bitsOpShift) == 0 {
			return GoVarAddr(v), true
		}
	}
	return
}

// FindType lookups a Go type by name.
func (p *GoPackage) FindType(name string) (typ reflect.Type, ok bool) {
	typ, ok = p.types[name]
	return
}

// Var creates a GoVarInfo instance.
func (p *GoPackage) Var(name string, addr interface{}) GoVarInfo {
	if log.CanOutput(log.Ldebug) {
		if reflect.TypeOf(addr).Kind() != reflect.Ptr {
			log.Panicln("variable address isn't a pointer?")
		}
	}
	return GoVarInfo{Pkg: p, Name: name, Addr: addr}
}

// Func creates a GoFuncInfo instance.
func (p *GoPackage) Func(name string, fn interface{}, exec func(i Instr, p *Context)) GoFuncInfo {
	return GoFuncInfo{Pkg: p, Name: name, This: fn, exec: exec}
}

// Funcv creates a GoFuncvInfo instance.
func (p *GoPackage) Funcv(name string, fn interface{}, exec func(i Instr, p *Context)) GoFuncvInfo {
	return GoFuncvInfo{GoFuncInfo{Pkg: p, Name: name, This: fn, exec: exec}, 0}
}

// Type creates a GoTypeInfo instance.
func (p *GoPackage) Type(name string, typ reflect.Type) GoTypeInfo {
	return GoTypeInfo{Pkg: p, Name: name, Type: typ}
}

// Rtype gets the real type information.
func (p *GoPackage) Rtype(typ reflect.Type) GoTypeInfo {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	return GoTypeInfo{Pkg: p, Name: typ.Name(), Type: typ}
}

// RegisterVars registers all exported Go variables of this package.
func (p *GoPackage) RegisterVars(vars ...GoVarInfo) (base GoVarAddr) {
	base = GoVarAddr(len(govars))
	govars = append(govars, vars...)
	for i, v := range vars {
		p.syms[v.Name] = uint32(base) + uint32(i)
	}
	return
}

// RegisterFuncs registers all exported Go functions of this package.
func (p *GoPackage) RegisterFuncs(funs ...GoFuncInfo) (base GoFuncAddr) {
	if log.CanOutput(log.Ldebug) {
		for _, v := range funs {
			if v.Pkg != p {
				log.Panicln("function doesn't belong to this package:", v.Name)
			}
			if v.This != nil && reflect.TypeOf(v.This).IsVariadic() {
				log.Panicln("function is variadic? -", v.Name)
			}
		}
	}
	base = GoFuncAddr(len(gofuns))
	gofuns = append(gofuns, funs...)
	for i, v := range funs {
		p.syms[v.Name] = (uint32(base) + uint32(i)) | (opCallGoFunc << bitsOpShift)
	}
	return
}

// RegisterFuncvs registers all exported Go functions with variadic arguments of this package.
func (p *GoPackage) RegisterFuncvs(funs ...GoFuncvInfo) (base GoFuncvAddr) {
	if log.CanOutput(log.Ldebug) {
		for _, v := range funs {
			if v.Pkg != p {
				log.Panicln("function doesn't belong to this package:", v.Name)
			}
			if v.This != nil && !reflect.TypeOf(v.This).IsVariadic() {
				log.Panicln("function isn't variadic? -", v.Name)
			}
		}
	}
	base = GoFuncvAddr(len(gofunvs))
	gofunvs = append(gofunvs, funs...)
	for i, v := range funs {
		p.syms[v.Name] = (uint32(base) + uint32(i)) | (opCallGoFuncv << bitsOpShift)
	}
	return
}

// RegisterTypes registers all exported Go types defined by this package.
func (p *GoPackage) RegisterTypes(typinfos ...GoTypeInfo) {
	for _, ti := range typinfos {
		if p != ti.Pkg {
			log.Panicln("RegisterTypes failed: unmatched package instance.")
		}
		if ti.Name == "" {
			log.Panicln("RegisterTypes failed: unnamed type? -", ti.Type)
		}
		if _, ok := p.types[ti.Name]; ok {
			log.Panicln("RegisterTypes failed: register an existed type -", p.PkgPath, ti.Name)
		}
		p.types[ti.Name] = ti.Type
	}
}

// -----------------------------------------------------------------------------

var (
	gopkgs  = make(map[string]*GoPackage)
	gofuns  []GoFuncInfo
	gofunvs []GoFuncvInfo
	govars  []GoVarInfo
)

// GoFuncAddr represents a Go function address.
type GoFuncAddr uint32

// GoFuncvAddr represents a variadic Go function address.
type GoFuncvAddr uint32

// GoVarAddr represents a variadic Go variable address.
type GoVarAddr uint32

// GoFuncInfo represents a Go function information.
type GoFuncInfo struct {
	Pkg  *GoPackage
	Name string
	This interface{}
	exec func(i Instr, p *Context)
}

// GoFuncvInfo represents a Go function information.
type GoFuncvInfo struct {
	GoFuncInfo
	numIn int // cache
}

func (p *GoFuncvInfo) getNumIn() int {
	if p.numIn == 0 {
		p.numIn = reflect.TypeOf(p.This).NumIn()
	}
	return p.numIn
}

// GoTypeInfo represents a Go type information.
type GoTypeInfo struct {
	Pkg  *GoPackage
	Name string
	Type reflect.Type
}

// GoVarInfo represents a Go variable information.
type GoVarInfo struct {
	Pkg  *GoPackage
	Name string
	Addr interface{}
}

// GetInfo retuns a Go function info.
func (i GoFuncAddr) GetInfo() *GoFuncInfo {
	if i < GoFuncAddr(len(gofuns)) {
		return &gofuns[i]
	}
	return nil
}

// GetInfo retuns a Go function info.
func (i GoFuncvAddr) GetInfo() *GoFuncInfo {
	if i < GoFuncvAddr(len(gofunvs)) {
		return &gofunvs[i].GoFuncInfo
	}
	return nil
}

// GetInfo retuns a Go variable info.
func (i GoVarAddr) GetInfo() *GoVarInfo {
	if i < GoVarAddr(len(govars)) {
		return &govars[i]
	}
	return nil
}

// CallGoFunc instr
func (p *Builder) CallGoFunc(fun GoFuncAddr) *Builder {
	p.code.data = append(p.code.data, (opCallGoFunc<<bitsOpShift)|uint32(fun))
	return p
}

// CallGoFuncv instr
func (p *Builder) CallGoFuncv(fun GoFuncvAddr, arity int) *Builder {
	code := p.code
	if arity < 0 {
		arity = bitsFuncvArityVar
	} else if arity >= bitsFuncvArityMax {
		p.Push(arity - bitsFuncvArityMax)
		arity = bitsFuncvArityMax
	}
	i := (opCallGoFuncv << bitsOpShift) | (uint32(arity) << bitsOpCallFuncvShift) | uint32(fun)
	code.data = append(code.data, i)
	return p
}

// -----------------------------------------------------------------------------
