package codegen

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"go/format"
	"path/filepath"

	"github.com/gogo/protobuf/protoc-gen-gogo/generator"
	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/util"
)

// Generator for codegen
type Generator struct {
	serviceName string
	rpcCmd      qrpc.Cmd
	errCmd      qrpc.Cmd
	services    map[string]map[string]reflect.Value
	buff        bytes.Buffer
}

const (
	defaultRPCCmd qrpc.Cmd = 10000 + iota
	defaultRPCErrCmd
)

// New creates a Generator with default cmds
func New(serviceName string) *Generator {
	return NewWithCmd(serviceName, defaultRPCCmd, defaultRPCErrCmd)
}

// NewWithCmd creates a Generator with specified cmds
func NewWithCmd(serviceName string, rpcCmd, errCmd qrpc.Cmd) *Generator {
	return &Generator{serviceName: generator.CamelCase(serviceName), rpcCmd: rpcCmd, errCmd: errCmd, services: make(map[string]map[string]reflect.Value)}
}

// Register for the main service
func (g *Generator) Register(s interface{}) {
	// empty ns for main service
	g.registerSub("", s)
}

// RegisterSub for sub service, if any
func (g *Generator) RegisterSub(ns string, s interface{}) {
	if ns == "" {
		panic("empty ns for RegisterSub")
	}
	g.registerSub(ns, s)
}

func (g *Generator) registerSub(ns string, s interface{}) {
	methods := util.ScanMethods(s)
	if len(methods) == 0 {
		panic(fmt.Sprintf("no method scanned for %s", ns))
	}
	for _, method := range methods {
		checkFuncIO(method)
	}

	ns = generator.CamelCase(ns)
	if _, ok := g.services[ns]; ok {
		panic(fmt.Sprintf("service %s added twice", ns))
	}

	g.services[ns] = methods
}

var ctxType = util.TypeByPointer((*context.Context)(nil))
var outputType = util.TypeByPointer((*Output)(nil))
var marshalerType = util.TypeByPointer((*Marshaler)(nil))

func checkFuncIO(fun reflect.Value) {
	inTypes := util.FuncInputTypes(fun)
	if len(inTypes) != 2 {
		panic(fmt.Sprintf("input count not 2:%v", fun))
	}
	if !inTypes[0].Implements(ctxType) {
		panic(fmt.Sprintf("first input not impl context.Context:%v", fun))
	}

	outTypes := util.FuncOutputTypes(fun)
	if len(outTypes) != 1 {
		panic(fmt.Sprintf("output count not 1:%v", fun))
	}

	if !reflect.PtrTo(outTypes[0]).Implements(outputType) {
		panic(fmt.Sprintf("output not impl Output:%v %v", fun, outTypes[0]))
	}
}

const (
	contextPkg = "context"
	qrpcPkg    = "github.com/zhiqiangxu/qrpc"
	codegenPkg = "github.com/zhiqiangxu/qrpc/codegen"
	jsonPkg    = "encoding/json"
	fmtPkg     = "fmt"
)

var (
	contextPkgAlias string
	qrpcPkgAlias    string
	codegenPkgAlias string
	jsonPkgAlias    string
	fmtPkgAlias     string
	allPkgs         map[string]string
)

func (g *Generator) generatePkg() {
	dir, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("Getwd err:%v", err))
	}
	g.P("package ", filepath.Base(dir))
}

func (g *Generator) generateImports() {

	allPkgs = make(map[string]string)
	for _, methods := range g.services {
		for _, method := range methods {
			inTypes := util.FuncInputTypes(method)
			outTypes := util.FuncOutputTypes(method)
			inTypePkg := inTypes[1].PkgPath()
			outTypePkg := outTypes[0].PkgPath()
			if inTypePkg != "" {
				if _, ok := allPkgs[inTypePkg]; !ok {
					allPkgs[inTypePkg] = generator.RegisterUniquePackageName(g.pkgLastName(inTypePkg), nil)
				}
			}

			if outTypePkg != "" {
				if _, ok := allPkgs[outTypePkg]; !ok {
					allPkgs[outTypePkg] = generator.RegisterUniquePackageName(g.pkgLastName(outTypePkg), nil)
				}
			}

		}
	}

	for _, pkg := range []string{contextPkg, qrpcPkg, codegenPkg, fmtPkg, jsonPkg} {
		if _, ok := allPkgs[pkg]; !ok {
			allPkgs[pkg] = generator.RegisterUniquePackageName(g.pkgLastName(pkg), nil)
		}
	}

	g.P("import (")
	for fqPkg, alias := range allPkgs {
		g.P(alias, " ", generator.GoImportPath(fqPkg))
	}
	g.P(")")

	contextPkgAlias = allPkgs[contextPkg]
	qrpcPkgAlias = allPkgs[qrpcPkg]
	codegenPkgAlias = allPkgs[codegenPkg]
	fmtPkgAlias = allPkgs[fmtPkg]
	jsonPkgAlias = allPkgs[jsonPkg]
}

func (g *Generator) pkgLastName(fqPkg string) string {
	parts := strings.Split(fqPkg, "/")
	return parts[len(parts)-1]
}

// Generate for generating code
func (g *Generator) Generate() {

	g.generatePkg()
	g.generateImports()

	// client part

	// main service client
	// interface
	g.P("type ", g.serviceName, "Client interface {")
	for ns, methods := range g.services {
		if ns == "" {
			// main service method
			for name, method := range methods {
				g.generateClientSignature(name, method)
			}
		} else {
			// sub service client
			g.P(ns, "Client() ", g.serviceName, ns, "Client")
		}
	}
	g.P("}")
	g.P()
	// struct
	structName := unexport(g.serviceName) + "Client"
	g.P("type ", structName, " struct {")
	g.P("	*", codegenPkgAlias, ".Client")
	g.P("}")
	g.P()
	g.P("func New", g.serviceName, "Client(addrs []string, conf ", qrpcPkgAlias, ".ConnectionConfig) ", g.serviceName, "Client {")
	g.P("	return ", structName, "{", codegenPkgAlias, ".NewClient(", qrpcPkgAlias, ".Cmd(", g.rpcCmd, "),", qrpcPkgAlias, ".Cmd(", g.errCmd, "), addrs, conf)}")
	g.P("}")
	g.P()

	// struct method
	for name, method := range g.services[""] {
		g.generateClientMethod("", name, method)
	}
	// generate ns Client method for main service client
	for ns := range g.services {
		if ns == "" {
			continue
		}
		g.P("func (c ", unexport(g.serviceName), "Client) ", ns, "Client {")
		g.P("	return ", unexport(g.serviceName), ns, "{c.Client}")
		g.P("}")
		g.P()
	}

	// sub service client
	for ns, methods := range g.services {
		if ns == "" {
			continue
		}

		// interface
		g.P("type ", g.serviceName, ns, "Client interface {")
		for name, method := range methods {
			g.generateClientSignature(name, method)
		}
		g.P("}")
		g.P()

		// struct
		g.P("type ", unexport(g.serviceName), ns, "Client struct {")
		g.P("	*", codegenPkgAlias, ".Client")
		g.P("}")
		g.P()

		// struct method
		for name, method := range methods {
			g.generateClientMethod(ns, name, method)
		}
	}

	// service mux part
	g.P("type ", g.serviceName, "ServiceMux interface {")
	g.P("	Register(", g.serviceName, "Service)")
	g.P("	RegisterSub(string, interface{})")
	g.P("	Mux() *", qrpcPkgAlias, ".ServeMux")
	g.P("}")
	g.P()
	g.P("type ", g.serviceName, "Service interface {")
	// struct method
	for name, method := range g.services[""] {
		g.generateClientSignature(name, method)
	}
	g.P("}")
	g.P()
	g.P("type ", unexport(g.serviceName), "ServiceMux struct {")
	g.P("	callback map[string]", codegenPkgAlias, ".MethodCall")
	g.P("	mux *", qrpcPkgAlias, ".ServeMux")
	g.P("}")
	g.P()
	g.P("func New", g.serviceName, "ServiceMux() ", g.serviceName, "ServiceMux {")
	g.P("	return &", unexport(g.serviceName), "ServiceMux{callback:make(map[string]", codegenPkgAlias, ".MethodCall)}")
	g.P("}")
	g.P()
	g.P("func (m *", unexport(g.serviceName), "ServiceMux) Register(s ", g.serviceName, "Service) {")
	for name, method := range g.services[""] {
		g.P("m.callback[", codegenPkgAlias, ".FQMethod(\"\",", strconv.Quote(name), ")] = func(ctx context.Context, inBytes []byte) (outBytes []byte, err error) {")
		inTypes := util.FuncInputTypes(method)
		outTypes := util.FuncInputTypes(method)
		inTypePtrIsMarshaler := reflect.PtrTo(inTypes[1]).Implements(marshalerType)
		outTypePtrIsMarshaler := reflect.PtrTo(outTypes[0]).Implements(marshalerType)
		if inTypes[1].PkgPath() != "" {
			alias := allPkgs[inTypes[1].PkgPath()]
			g.P("	var input ", alias, ".", inTypes[1].Name())
		} else {
			g.P("	var input ", inTypes[1].Name())
		}

		if inTypePtrIsMarshaler {
			g.P("err = input.Unmarshal(inBytes)")
		} else {
			g.P("err = ", jsonPkgAlias, ".Unmarshal(inBytes, &input)")
		}
		g.P("	if err != nil {")
		g.P("		return")
		g.P("	}")
		g.P("	output := s.", name, "(ctx, input)")
		if outTypePtrIsMarshaler {
			g.P("outBytes, err = output.Marshal()")
		} else {
			g.P("outBytes, err = ", jsonPkgAlias, ".Marshal(output)")
		}

		g.P("	return")

		g.P("}")
	}
	g.P("}")
	g.P()
	g.P("func (m *", unexport(g.serviceName), "ServiceMux) RegisterSub(ns string, ss interface{}) {")
	g.P("	switch ns {")
	for ns, methods := range g.services {
		if ns == "" {
			continue
		}

		g.P("case ", strconv.Quote(ns), ":")
		for name, method := range methods {
			g.P("	ssInterface := ss.(", g.serviceName, ns, "Client)")
			inTypes := util.FuncInputTypes(method)
			outTypes := util.FuncInputTypes(method)
			inTypePtrIsMarshaler := reflect.PtrTo(inTypes[1]).Implements(marshalerType)
			outTypePtrIsMarshaler := reflect.PtrTo(outTypes[0]).Implements(marshalerType)
			g.P("	m.callback[", codegenPkgAlias, ".FQMethod(", strconv.Quote(ns), ",", strconv.Quote(name), ")] = func(ctx context.Context, inBytes []byte) (outBytes []byte, err error) {")
			if inTypes[1].PkgPath() != "" {
				alias := allPkgs[inTypes[1].PkgPath()]
				g.P("		var input ", alias, ".", inTypes[1].Name())
			} else {
				g.P("		var input ", inTypes[1].Name())
			}

			if inTypePtrIsMarshaler {
				g.P("	err = input.Unmarshal(inBytes)")
			} else {
				g.P("	err = ", jsonPkgAlias, ".Unmarshal(inBytes, &input)")
			}
			g.P("		if err != nil {")
			g.P("			return")
			g.P("		}")
			g.P("		output := ssInterface.", name, "(ctx,input)")
			if outTypePtrIsMarshaler {
				g.P("	outBytes, err = output.Marshal()")
			} else {
				g.P("	outBytes, err = ", jsonPkgAlias, ".Marshal(output)")
			}
			g.P("		return")
			g.P("	}")
		}

	}
	g.P("	default:")
	g.P("		panic(", fmtPkgAlias, ".Sprintf(\"unknown ns:%v\",ns))")
	g.P("	}")
	g.P("}")
	g.P()
	g.P("func (m *", unexport(g.serviceName), "ServiceMux) Mux() *qrpc.ServeMux {")
	g.P("	if m.mux != nil {")
	g.P("		return m.mux")
	g.P("	}")
	g.P("	mux := ", qrpcPkgAlias, ".NewServeMux()")
	g.P("	mux.Handle(", qrpcPkgAlias, ".Cmd(", g.rpcCmd, "), ", codegenPkgAlias, ".NewServiceHandler(", qrpcPkgAlias, ".Cmd(", g.errCmd, "), m.callback))")
	g.P("	m.mux = mux")
	g.P("	return mux")
	g.P("}")
	g.P()
}

func unexport(s string) string { return strings.ToLower(s[:1]) + s[1:] }

func (g *Generator) generateClientSignature(name string, method reflect.Value) {
	inTypes := util.FuncInputTypes(method)
	outTypes := util.FuncOutputTypes(method)

	var inFQType, outFQType string
	if inTypes[1].PkgPath() != "" {
		inFQType = allPkgs[inTypes[1].PkgPath()] + "." + inTypes[1].Name()
	} else {
		inFQType = inTypes[1].Name()
	}
	if outTypes[0].PkgPath() != "" {
		outFQType = allPkgs[outTypes[0].PkgPath()] + "." + outTypes[0].Name()
	} else {
		outFQType = outTypes[0].Name()
	}
	g.P(name, "(", contextPkgAlias, ".Context,", inFQType, ")", outFQType)
}

func (g *Generator) generateClientMethod(ns, name string, method reflect.Value) {
	inTypes := util.FuncInputTypes(method)
	outTypes := util.FuncOutputTypes(method)
	inTypePtrIsMarshaler := reflect.PtrTo(inTypes[1]).Implements(marshalerType)
	outTypePtrIsMarshaler := reflect.PtrTo(outTypes[0]).Implements(marshalerType)

	var inFQType, outFQType string
	if inTypes[1].PkgPath() != "" {
		inFQType = allPkgs[inTypes[1].PkgPath()] + "." + inTypes[1].Name()
	} else {
		inFQType = inTypes[1].Name()
	}
	if outTypes[0].PkgPath() != "" {
		outFQType = allPkgs[outTypes[0].PkgPath()] + "." + outTypes[0].Name()
	} else {
		outFQType = outTypes[0].Name()
	}

	g.P("func (c ", unexport(g.serviceName), ns, "Client)", name, "(ctx ", contextPkgAlias, ".Context, input ", inFQType, ") (output ", outFQType, ") {")
	g.P("	cc := c.Client")
	if inTypePtrIsMarshaler {
		g.P("	inBytes, err := input.Marshal()")
	} else {
		g.P("	inBytes, err := ", jsonPkgAlias, ".Marshal(input)")
	}

	g.P("	if err != nil {")
	g.P("		output.SetError(err)")
	g.P("		return")
	g.P("	}")
	g.P("	outBytes, err := cc.Request(ctx, ", strconv.Quote(ns), ",", strconv.Quote(name), ", inBytes)")
	g.P("	if err != nil {")
	g.P("		output.SetError(err)")
	g.P("		return")
	g.P("	}")
	if outTypePtrIsMarshaler {
		g.P("err = output.Unmarshal(outBytes)")
	} else {
		g.P("err = ", jsonPkgAlias, ".Unmarshal(outBytes, &output)")
	}
	g.P("	if err != nil {")
	g.P("		output.SetError(err)")
	g.P("	}")
	g.P("	return")
	g.P("}")
}

// P for print stuff
func (g *Generator) P(args ...interface{}) {
	for _, arg := range args {
		g.buff.Write([]byte(fmt.Sprintf("%v", arg)))
	}
	g.buff.Write([]byte("\n"))
}

// Output the code
func (g *Generator) Output() {
	// gofmt
	outBytes, err := format.Source([]byte(g.buff.String()))
	if err != nil {
		fmt.Println(g.buff.String())
		panic(fmt.Sprintf("Output err:%v", err))
	}
	file, err := os.Create(unexport(g.serviceName) + ".cg.go")

	if err != nil {
		panic(fmt.Sprintf("Output Create err:%v", err))
	}
	defer file.Close()

	_, err = file.Write(outBytes)
	if err != nil {
		panic(fmt.Sprintf("Output Write err:%v", err))
	}
}
