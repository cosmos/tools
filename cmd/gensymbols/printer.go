package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/token"
	"go/types"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type Printer struct {
	output io.Writer

	features map[string]bool
	scope    []string
	current  *types.Package
}

func NewPrinter(output io.Writer) Printer {
	return Printer{
		output: output,

		features: map[string]bool{},
	}
}

func (p *Printer) Print(packages []*packages.Package) {
	p.features = map[string]bool{}

	for _, pkg := range packages {
		thePkg := pkg.Types
		scope := thePkg.Scope()
		for _, name := range scope.Names() {
			if token.IsExported(name) {
				p.printObj(scope.Lookup(name))
			}
		}
	}

	var featureCtx = make(map[string]map[string]bool) // feature -> context name -> true
	ctxName := packages[0].PkgPath
	for _, f := range p.Features() {
		if featureCtx[f] == nil {
			featureCtx[f] = make(map[string]bool)
		}
		featureCtx[f][ctxName] = true
	}

	var features []string
	for f, cmap := range featureCtx {
		comma := strings.Index(f, ",")
		for cname := range cmap {
			f2 := fmt.Sprintf("pkg %s%s", cname, f[comma:])
			features = append(features, f2)
		}
	}

	fail := false
	defer func() {
		if fail {
			os.Exit(1)
		}
	}()

	bw := bufio.NewWriter(p.output)
	defer bw.Flush()

	sort.Strings(features)
	for _, f := range features {
		fmt.Fprintln(bw, f)
	}
}

func (p *Printer) Features() (fs []string) {
	for f := range p.features {
		fs = append(fs, f)
	}
	sort.Strings(fs)
	return
}

func (p *Printer) printObj(obj types.Object) {
	switch obj := obj.(type) {
	case *types.Const:
		p.emitf("const %s %s", obj.Name(), p.typeString(obj.Type()))
		x := obj.Val()
		short := x.String()
		exact := x.ExactString()
		if short == exact {
			p.emitf("const %s = %s", obj.Name(), short)
		} else {
			p.emitf("const %s = %s  // %s", obj.Name(), short, exact)
		}
	case *types.Var:
		p.emitf("var %s %s", obj.Name(), p.typeString(obj.Type()))
	case *types.TypeName:
		p.emitType(obj)
	case *types.Func:
		p.emitFunc(obj)
	default:
		panic("unknown object: " + obj.String())
	}
}

func (p *Printer) typeString(typ types.Type) string {
	var buf bytes.Buffer
	p.writeType(&buf, typ)
	return buf.String()
}

func (p *Printer) writeType(buf *bytes.Buffer, typ types.Type) {
	switch typ := typ.(type) {
	case *types.Basic:
		s := typ.Name()
		switch typ.Kind() {
		case types.UnsafePointer:
			s = "unsafe.Pointer"
		case types.UntypedBool:
			s = "ideal-bool"
		case types.UntypedInt:
			s = "ideal-int"
		case types.UntypedRune:
			// "ideal-char" for compatibility with old tool
			// TODO(gri) change to "ideal-rune"
			s = "ideal-char"
		case types.UntypedFloat:
			s = "ideal-float"
		case types.UntypedComplex:
			s = "ideal-complex"
		case types.UntypedString:
			s = "ideal-string"
		case types.UntypedNil:
			panic("should never see untyped nil type")
		default:
			switch s {
			case "byte":
				s = "uint8"
			case "rune":
				s = "int32"
			}
		}
		buf.WriteString(s)

	case *types.Array:
		fmt.Fprintf(buf, "[%d]", typ.Len())
		p.writeType(buf, typ.Elem())

	case *types.Slice:
		buf.WriteString("[]")
		p.writeType(buf, typ.Elem())

	case *types.Struct:
		buf.WriteString("struct")

	case *types.Pointer:
		buf.WriteByte('*')
		p.writeType(buf, typ.Elem())

	case *types.Tuple:
		panic("should never see a tuple type")

	case *types.Signature:
		buf.WriteString("func")
		p.writeSignature(buf, typ)

	case *types.Interface:
		buf.WriteString("interface{")
		if typ.NumMethods() > 0 {
			buf.WriteByte(' ')
			buf.WriteString(strings.Join(sortedMethodNames(typ), ", "))
			buf.WriteByte(' ')
		}
		buf.WriteString("}")

	case *types.Map:
		buf.WriteString("map[")
		p.writeType(buf, typ.Key())
		buf.WriteByte(']')
		p.writeType(buf, typ.Elem())

	case *types.Chan:
		var s string
		switch typ.Dir() {
		case types.SendOnly:
			s = "chan<- "
		case types.RecvOnly:
			s = "<-chan "
		case types.SendRecv:
			s = "chan "
		default:
			panic("unreachable")
		}
		buf.WriteString(s)
		p.writeType(buf, typ.Elem())

	case *types.Named:
		obj := typ.Obj()
		pkg := obj.Pkg()
		if pkg != nil && pkg != p.current {
			buf.WriteString(pkg.Name())
			buf.WriteByte('.')
		}
		buf.WriteString(typ.Obj().Name())

	default:
		panic(fmt.Sprintf("unknown type %T", typ))
	}
}

func sortedMethodNames(typ *types.Interface) []string {
	n := typ.NumMethods()
	list := make([]string, n)
	for i := range list {
		list[i] = typ.Method(i).Name()
	}
	sort.Strings(list)
	return list
}

func (p *Printer) writeSignature(buf *bytes.Buffer, sig *types.Signature) {
	p.writeParams(buf, sig.Params(), sig.Variadic())
	switch res := sig.Results(); res.Len() {
	case 0:
		// nothing to do
	case 1:
		buf.WriteByte(' ')
		p.writeType(buf, res.At(0).Type())
	default:
		buf.WriteByte(' ')
		p.writeParams(buf, res, false)
	}
}

func (p *Printer) writeParams(buf *bytes.Buffer, t *types.Tuple, variadic bool) {
	buf.WriteByte('(')
	for i, n := 0, t.Len(); i < n; i++ {
		if i > 0 {
			buf.WriteString(", ")
		}
		typ := t.At(i).Type()
		if variadic && i+1 == n {
			buf.WriteString("...")
			typ = typ.(*types.Slice).Elem()
		}
		p.writeType(buf, typ)
	}
	buf.WriteByte(')')
}

func (p *Printer) emitf(format string, args ...interface{}) {
	f := strings.Join(p.scope, ", ") + ", " + fmt.Sprintf(format, args...)
	if strings.Contains(f, "\n") {
		panic("feature contains newlines: " + f)
	}

	if _, dup := p.features[f]; dup {
		return
		//panic("duplicate feature inserted: " + f)
	}
	p.features[f] = true
}

func (p *Printer) emitType(obj *types.TypeName) {
	name := obj.Name()
	typ := obj.Type()
	switch typ := typ.Underlying().(type) {
	case *types.Struct:
		p.emitStructType(name, typ)
	case *types.Interface:
		p.emitIfaceType(name, typ)
		return // methods are handled by emitIfaceType
	default:
		p.emitf("type %s %s", name, p.typeString(typ.Underlying()))
	}

	// emit methods with value receiver
	var methodNames map[string]bool
	vset := types.NewMethodSet(typ)
	for i, n := 0, vset.Len(); i < n; i++ {
		m := vset.At(i)
		if m.Obj().Exported() {
			p.emitMethod(m)
			if methodNames == nil {
				methodNames = make(map[string]bool)
			}
			methodNames[m.Obj().Name()] = true
		}
	}

	// emit methods with pointer receiver; exclude
	// methods that we have emitted already
	// (the method set of *T includes the methods of T)
	pset := types.NewMethodSet(types.NewPointer(typ))
	for i, n := 0, pset.Len(); i < n; i++ {
		m := pset.At(i)
		if m.Obj().Exported() && !methodNames[m.Obj().Name()] {
			p.emitMethod(m)
		}
	}
}

func (p *Printer) emitStructType(name string, typ *types.Struct) {
	typeStruct := fmt.Sprintf("type %s struct", name)
	p.emitf(typeStruct)
	defer p.pushScope(typeStruct)()

	for i := 0; i < typ.NumFields(); i++ {
		f := typ.Field(i)
		if !f.Exported() {
			continue
		}
		typ := f.Type()
		if f.Anonymous() {
			p.emitf("embedded %s", p.typeString(typ))
			continue
		}
		p.emitf("%s %s", f.Name(), p.typeString(typ))
	}
}

// pushScope enters a new scope (walking a package, type, node, etc)
// and returns a function that will leave the scope (with sanity checking
// for mismatched pushes & pops)
func (p *Printer) pushScope(name string) (popFunc func()) {
	p.scope = append(p.scope, name)
	return func() {
		if len(p.scope) == 0 {
			log.Fatalf("attempt to leave scope %q with empty scope list", name)
		}
		if p.scope[len(p.scope)-1] != name {
			log.Fatalf("attempt to leave scope %q, but scope is currently %#v", name, p.scope)
		}
		p.scope = p.scope[:len(p.scope)-1]
	}
}

func (p *Printer) emitFunc(f *types.Func) {
	sig := f.Type().(*types.Signature)
	if sig.Recv() != nil {
		panic("method considered a regular function: " + f.String())
	}
	p.emitf("func %s%s", f.Name(), p.signatureString(sig))
}

func (p *Printer) signatureString(sig *types.Signature) string {
	var buf bytes.Buffer
	p.writeSignature(&buf, sig)
	return buf.String()
}

func (p *Printer) emitMethod(m *types.Selection) {
	sig := m.Type().(*types.Signature)
	recv := sig.Recv().Type()
	// report exported methods with unexported receiver base type
	if true {
		base := recv
		if p, _ := recv.(*types.Pointer); p != nil {
			base = p.Elem()
		}
		if obj := base.(*types.Named).Obj(); !obj.Exported() {
			log.Fatalf("exported method with unexported receiver base type: %s", m)
		}
	}
	p.emitf("method (%s) %s%s", p.typeString(recv), m.Obj().Name(), p.signatureString(sig))
}

func (p *Printer) emitIfaceType(name string, typ *types.Interface) {
	pop := p.pushScope(", type " + name + " interface")

	var methodNames []string
	complete := true
	mset := types.NewMethodSet(typ)
	for i, n := 0, mset.Len(); i < n; i++ {
		m := mset.At(i).Obj().(*types.Func)
		if !m.Exported() {
			complete = false
			continue
		}
		methodNames = append(methodNames, m.Name())
		p.emitf("%s%s", m.Name(), p.signatureString(m.Type().(*types.Signature)))
	}

	if !complete {
		// The method set has unexported methods, so all the
		// implementations are provided by the same package,
		// so the method set can be extended. Instead of recording
		// the full set of names (below), record only that there were
		// unexported methods. (If the interface shrinks, we will notice
		// because a method signature emitted during the last loop
		// will disappear.)
		p.emitf("unexported methods")
	}

	pop()

	if !complete {
		return
	}

	if len(methodNames) == 0 {
		p.emitf("type %s interface {}", name)
		return
	}

	sort.Strings(methodNames)
	p.emitf("type %s interface { %s }", name, strings.Join(methodNames, ", "))
}
