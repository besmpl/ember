package rubyproof

import (
	"bytes"
	"fmt"
	"go/format"
	gotoken "go/token"
	"strconv"
	"text/template"

	"github.com/besmpl/ember/preparedsource"
)

type emissionPlan struct {
	Lookup lookupEmissionPlan

	InitialValue, OtherValue, BetaValue, GammaValue int64
	InitialLabelMark, InitialLabelReturn            int64
	BetaLabelMark, BetaLabelReturn                  int64
	GammaLabelMark, GammaLabelReturn                int64
	RedefinedLabelMark, RedefinedLabelReturn        int64
	IncludedLabelMark, IncludedLabelReturn          int64
	PrependedLabelMark, PrependedLabelReturn        int64
	IncludedRedefinedMark, IncludedRedefinedReturn  int64
	TopologyValue                                   int64
	SingletonValue                                  int64
	SingletonLabelMark, SingletonLabelReturn        int64
	ReturnMark, ReturnAdd, ReturnEnsureMark         int64
	RaiseMark                                       int64
	RaiseMessage                                    string
	PublicRejected                                  int64
	RescueMark, RescueAdd, ApplyEnsureMark          int64
	ApplyEnsureAdd                                  int64
}

// emitPreparedSource admits only the proven semantic shape, then lowers its
// checked facts into one direct, dependency-free Go package. Unsupported
// checked shapes fail explicitly instead of silently changing Ruby behavior.
func emitPreparedSource(program *checkedProgram, packageName string) (preparedsource.Set, error) {
	if !validGeneratedPackageName(packageName) {
		return preparedsource.Set{}, fmt.Errorf("ruby: invalid generated Go package name %q", packageName)
	}
	plan, err := buildEmissionPlan(program)
	if err != nil {
		return preparedsource.Set{}, err
	}
	data := struct {
		Package string
		Plan    emissionPlan
	}{Package: packageName, Plan: plan}
	parsed, err := template.New("ruby-generated").Funcs(template.FuncMap{"quote": strconv.Quote}).Parse(preparedTemplate)
	if err != nil {
		return preparedsource.Set{}, fmt.Errorf("ruby: parse private prepared template: %w", err)
	}
	var raw bytes.Buffer
	if err := parsed.Execute(&raw, data); err != nil {
		return preparedsource.Set{}, fmt.Errorf("ruby: execute private prepared template: %w", err)
	}
	formatted, err := format.Source(raw.Bytes())
	if err != nil {
		return preparedsource.Set{}, fmt.Errorf("ruby: format generated Go: %w\n%s", err, raw.String())
	}
	return preparedsource.NewSet(packageName, []preparedsource.File{{Name: "ruby_generated.go", Kind: preparedsource.GoFile, Content: string(formatted)}})
}

func validGeneratedPackageName(name string) bool {
	if name == "" || !gotoken.IsIdentifier(name) || gotoken.Lookup(name).IsKeyword() || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for index := 1; index < len(name); index++ {
		character := name[index]
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_') {
			return false
		}
	}
	return true
}

func buildEmissionPlan(program *checkedProgram) (emissionPlan, error) {
	if program == nil || len(program.classOrder) != 7 || program.initialLabelMethod == nil || program.redefinedLabelMethod == nil {
		return emissionPlan{}, fmt.Errorf("ruby: checked program is outside prepared proof shape")
	}
	if err := validateTopLevelShape(program); err != nil {
		return emissionPlan{}, err
	}
	base, beta, gamma := program.classes["Base"], program.classes["Beta"], program.classes["Gamma"]
	included, prepended := program.classes["IncludedLabel"], program.classes["PrependedLabel"]
	if base == nil || beta == nil || gamma == nil || included == nil || prepended == nil {
		return emissionPlan{}, fmt.Errorf("ruby: checked owner roles are outside prepared proof")
	}
	plan := emissionPlan{}
	var err error
	plan.Lookup, err = buildLookupEmissionPlan(program)
	if err != nil {
		return emissionPlan{}, err
	}
	plan.InitialLabelMark, plan.InitialLabelReturn, err = extractLabel(program.initialLabelMethod)
	if err != nil {
		return emissionPlan{}, err
	}
	plan.BetaLabelMark, plan.BetaLabelReturn, err = extractLabel(beta.methods["label"][0])
	if err != nil {
		return emissionPlan{}, err
	}
	plan.GammaLabelMark, plan.GammaLabelReturn, err = extractLabel(gamma.methods["label"][0])
	if err != nil {
		return emissionPlan{}, err
	}
	plan.RedefinedLabelMark, plan.RedefinedLabelReturn, err = extractLabel(program.redefinedLabelMethod)
	if err != nil {
		return emissionPlan{}, err
	}
	plan.IncludedLabelMark, plan.IncludedLabelReturn, err = extractLabel(included.methods["label"][0])
	if err != nil {
		return emissionPlan{}, err
	}
	plan.PrependedLabelMark, plan.PrependedLabelReturn, err = extractLabel(prepended.methods["label"][0])
	if err != nil {
		return emissionPlan{}, err
	}
	plan.IncludedRedefinedMark, plan.IncludedRedefinedReturn, err = extractLabel(included.methods["label"][1])
	if err != nil {
		return emissionPlan{}, err
	}
	if err := validateInitialize(base.methods["initialize"][0]); err != nil {
		return emissionPlan{}, err
	}
	if err := validateSubclassInitialize(beta.methods["initialize"][0], "@beta", false); err != nil {
		return emissionPlan{}, err
	}
	if err := validateSubclassInitialize(gamma.methods["initialize"][0], "@gamma", true); err != nil {
		return emissionPlan{}, err
	}
	if err := validateMark(base.methods["mark"][0]); err != nil {
		return emissionPlan{}, err
	}
	for _, name := range []string{"hot", "value", "trace"} {
		if err := validateAccessor(base.methods[name][0], map[string]string{"hot": "@value", "value": "@value", "trace": "@trace"}[name]); err != nil {
			return emissionPlan{}, err
		}
	}
	plan.RescueMark, plan.RescueAdd, plan.ApplyEnsureMark, plan.ApplyEnsureAdd, err = extractApply(base.methods["apply"][0])
	if err != nil {
		return emissionPlan{}, err
	}
	plan.ReturnMark, plan.ReturnAdd, plan.ReturnEnsureMark, err = extractRunReturn(program.topMethods["run_return"][0])
	if err != nil {
		return emissionPlan{}, err
	}
	if err := validateReadLabel(program.topMethods["read_label"][0]); err != nil {
		return emissionPlan{}, err
	}
	if err := validateReadLabel(program.topMethods["read_singleton"][0]); err != nil {
		return emissionPlan{}, err
	}
	if err := validateReadSaved(program.topMethods["read_saved"][0]); err != nil {
		return emissionPlan{}, err
	}
	plan.PublicRejected, err = validateReadPublic(program.topMethods["read_public"][0])
	if err != nil {
		return emissionPlan{}, err
	}
	plan.InitialValue, plan.OtherValue, plan.BetaValue, plan.GammaValue, plan.RaiseMark, plan.RaiseMessage, err = extractTopLevelValues(program)
	if err != nil {
		return emissionPlan{}, err
	}
	plan.TopologyValue, err = extractTopologyValues(program)
	if err != nil {
		return emissionPlan{}, err
	}
	if program.singletonDefinition == nil || program.singletonDefinition.body == nil {
		return emissionPlan{}, fmt.Errorf("ruby: checked singleton definition is unavailable")
	}
	plan.SingletonLabelMark, plan.SingletonLabelReturn, err = extractLabel(program.singletonDefinition.body)
	if err != nil {
		return emissionPlan{}, err
	}
	plan.SingletonValue, err = extractSingletonValues(program)
	if err != nil {
		return emissionPlan{}, err
	}
	return plan, nil
}

func validateTopLevelShape(program *checkedProgram) error {
	items := program.module.statements
	if len(items) != 69 {
		return fmt.Errorf("ruby: prepared proof expects 69 top-level statements, got %d", len(items))
	}
	wantKinds := []statementKind{
		statementClass, statementClass, statementClass, statementClass,
		statementMethod, statementMethod, statementMethod, statementMethod,
		statementAssign, statementAssign, statementAssign, statementAssign, statementAssign,
		statementAssign, statementAssign, statementAssign, statementAssign, statementAssign,
		statementClass,
		statementAssign, statementAssign, statementAssign, statementAssign, statementAssign, statementAssign,
		statementClass, statementAssign, statementClass,
		statementAssign, statementAssign, statementAssign, statementClass, statementAssign,
		statementClass, statementAssign, statementClass, statementAssign,
		statementExpression, statementAssign,
		statementModule, statementModule, statementClass,
		statementAssign, statementAssign, statementAssign, statementAssign,
		statementClass, statementAssign, statementClass, statementAssign,
		statementModule, statementAssign, statementModule,
		statementAssign, statementAssign, statementAssign, statementAssign,
		statementMethod,
		statementAssign, statementAssign, statementAssign, statementAssign, statementAssign,
		statementExpression,
		statementAssign, statementAssign, statementAssign, statementAssign, statementAssign,
	}
	wantNames := []string{
		"Base", "Alpha", "Beta", "Gamma", "read_label", "read_saved", "read_public", "run_return",
		"alpha", "same", "other", "beta", "gamma", "before", "beta_before", "gamma_before", "alpha_warm", "beta_warm",
		"Base", "after", "alpha_after_hit", "beta_after", "gamma_after", "saved", "public_before",
		"Base", "protected_rejected", "Base", "private_rejected", "private_sent", "private_beta", "Base", "public_restored",
		"Beta", "beta_removed", "Gamma", "returned", "", "result",
		"IncludedLabel", "PrependedLabel", "Delta", "c1_alpha", "c1_beta", "c1_delta", "c1_delta_before",
		"Alpha", "c1_delta_included", "Beta", "c1_beta_route", "PrependedLabel", "c1_beta_defined", "IncludedLabel",
		"c1_delta_redefined", "c1_saved_stable", "c1_trace", "c1_result",
		"read_singleton", "c2_receiver", "c2_peer", "c2_before", "c2_warm", "c2_peer_before", "",
		"c2_after", "c2_after_hit", "c2_peer_after", "c2_trace", "c2_result",
	}
	for index := range items {
		if items[index].kind != wantKinds[index] || items[index].name != wantNames[index] {
			return fmt.Errorf("ruby: top-level statement %d is outside prepared proof shape", index+1)
		}
	}
	base, alpha, beta, gamma := items[0], items[1], items[2], items[3]
	reopenedBase, protectedBase, privateBase, publicBase := items[18], items[25], items[27], items[31]
	reopenedBeta, reopenedGamma := items[33], items[35]
	includedModule, emptyPrependedModule, delta := items[39], items[40], items[41]
	includeAlpha, prependBeta := items[46], items[48]
	definedPrependedModule, redefinedIncludedModule := items[50], items[52]
	if len(base.body) != 8 || len(alpha.body) != 0 || len(beta.body) != 2 || len(gamma.body) != 2 ||
		len(reopenedBase.body) != 1 || reopenedBase.body[0] != program.redefinedLabelMethod.declaration ||
		len(protectedBase.body) != 1 || len(privateBase.body) != 1 || len(publicBase.body) != 1 ||
		len(reopenedBeta.body) != 1 || len(reopenedGamma.body) != 1 ||
		len(includedModule.body) != 1 || len(emptyPrependedModule.body) != 0 || len(delta.body) != 0 ||
		len(includeAlpha.body) != 1 || len(prependBeta.body) != 1 || len(definedPrependedModule.body) != 1 || len(redefinedIncludedModule.body) != 1 {
		return fmt.Errorf("ruby: class/reopen shape is outside prepared proof")
	}
	included, prepended := program.classes["IncludedLabel"], program.classes["PrependedLabel"]
	if included == nil || prepended == nil || len(included.methods["label"]) != 2 || len(prepended.methods["label"]) != 1 ||
		includedModule.body[0] != included.methods["label"][0].declaration || redefinedIncludedModule.body[0] != included.methods["label"][1].declaration ||
		definedPrependedModule.body[0] != prepended.methods["label"][0].declaration {
		return fmt.Errorf("ruby: module definition shape is outside prepared proof")
	}
	alias := program.classOperations[base.body[3]]
	protected := program.classOperations[protectedBase.body[0]]
	private := program.classOperations[privateBase.body[0]]
	public := program.classOperations[publicBase.body[0]]
	remove := program.classOperations[reopenedBeta.body[0]]
	undef := program.classOperations[reopenedGamma.body[0]]
	include := program.classOperations[includeAlpha.body[0]]
	prepend := program.classOperations[prependBeta.body[0]]
	if alias == nil || alias.kind != lookupMutationAlias || alias.name != "saved_label" ||
		protected == nil || protected.kind != lookupMutationVisibility || protected.visibility != lookupVisibilityProtected ||
		private == nil || private.kind != lookupMutationVisibility || private.visibility != lookupVisibilityPrivate ||
		public == nil || public.kind != lookupMutationVisibility || public.visibility != lookupVisibilityPublic ||
		remove == nil || remove.kind != lookupMutationRemove || remove.name != "label" ||
		undef == nil || undef.kind != lookupMutationUndef || undef.name != "label" ||
		include == nil || include.kind != lookupMutationInclude || include.attachment == nil || include.attachment.module != included ||
		prepend == nil || prepend.kind != lookupMutationPrepend || prepend.attachment == nil || prepend.attachment.module != prepended {
		return fmt.Errorf("ruby: alias/remove/undef schedule is outside prepared proof")
	}
	result := items[38].expression
	if result.kind != expressionArray || len(result.items) != 24 {
		return fmt.Errorf("ruby: final result array shape is outside prepared proof")
	}
	wantResult := []string{
		"same", "other", "before", "beta_before", "gamma_before", "alpha_warm", "beta_warm", "after", "alpha_after_hit", "beta_after", "gamma_after", "saved",
		"public_before", "protected_rejected", "private_rejected", "private_sent", "private_beta", "public_restored", "beta_removed", "returned",
	}
	for index, name := range wantResult {
		if result.items[index].kind != expressionLocal || result.items[index].text != name {
			return fmt.Errorf("ruby: final result item %d is outside prepared proof", index+1)
		}
	}
	if !isReceiverCall(result.items[20], "alpha", "value", 0) || !isReceiverCall(result.items[21], "alpha", "trace", 0) ||
		!isReceiverCall(result.items[22], "beta", "trace", 0) || !isReceiverCall(result.items[23], "gamma", "trace", 0) {
		return fmt.Errorf("ruby: final object projections are outside prepared proof")
	}
	topologyResult := items[56].expression
	if topologyResult.kind != expressionArray || len(topologyResult.items) != 7 {
		return fmt.Errorf("ruby: topology result array shape is outside prepared proof")
	}
	wantTopologyResult := []string{
		"c1_delta_before", "c1_delta_included", "c1_beta_route", "c1_beta_defined", "c1_delta_redefined", "c1_saved_stable", "c1_trace",
	}
	for index, name := range wantTopologyResult {
		if topologyResult.items[index].kind != expressionLocal || topologyResult.items[index].text != name {
			return fmt.Errorf("ruby: topology result item %d is outside prepared proof", index+1)
		}
	}
	singletonResult := items[68].expression
	if singletonResult.kind != expressionArray || len(singletonResult.items) != 7 {
		return fmt.Errorf("ruby: singleton result array shape is outside prepared proof")
	}
	wantSingletonResult := []string{
		"c2_before", "c2_warm", "c2_peer_before", "c2_after", "c2_after_hit", "c2_peer_after", "c2_trace",
	}
	for index, name := range wantSingletonResult {
		if singletonResult.items[index].kind != expressionLocal || singletonResult.items[index].text != name {
			return fmt.Errorf("ruby: singleton result item %d is outside prepared proof", index+1)
		}
	}
	return nil
}

func extractLabel(method *checkedMethod) (int64, int64, error) {
	body := method.declaration.body
	if len(body) != 2 {
		return 0, 0, fmt.Errorf("ruby: label version %d body is outside prepared proof", method.version)
	}
	mark, ok := implicitIntegerCall(body[0], "mark")
	if !ok || body[1].kind != statementExpression || body[1].expression.kind != expressionInteger {
		return 0, 0, fmt.Errorf("ruby: label version %d body is outside prepared proof", method.version)
	}
	return mark, body[1].expression.integer, nil
}

func validateInitialize(method *checkedMethod) error {
	body := method.declaration.body
	if len(body) != 2 || len(method.declaration.parameters) != 1 ||
		!isInstanceAssignment(body[0], "@value", expressionLocal, method.declaration.parameters[0]) ||
		!isInstanceIntegerAssignment(body[1], "@trace", 0) {
		return fmt.Errorf("ruby: initialize body is outside prepared proof")
	}
	return nil
}

func validateSubclassInitialize(method *checkedMethod, privateField string, traceFirst bool) error {
	body := method.declaration.body
	if len(body) != 3 || len(method.declaration.parameters) != 1 {
		return fmt.Errorf("ruby: %s initialize body is outside prepared proof", method.owner.name)
	}
	parameter := method.declaration.parameters[0]
	want := func(item *statement, field string) bool {
		return isInstanceAssignment(item, field, expressionLocal, parameter)
	}
	valid := false
	if traceFirst {
		valid = isInstanceIntegerAssignment(body[0], "@trace", 0) && want(body[1], privateField) && want(body[2], "@value")
	} else {
		valid = want(body[0], privateField) && want(body[1], "@value") && isInstanceIntegerAssignment(body[2], "@trace", 0)
	}
	if !valid {
		return fmt.Errorf("ruby: %s initialize body is outside prepared proof", method.owner.name)
	}
	return nil
}

func validateMark(method *checkedMethod) error {
	body := method.declaration.body
	if len(body) != 1 || len(method.declaration.parameters) != 1 || body[0].kind != statementAssign || body[0].name != "@trace" {
		return fmt.Errorf("ruby: mark body is outside prepared proof")
	}
	value := body[0].expression
	if value.kind != expressionBinary || value.text != "+" || value.left.kind != expressionBinary || value.left.text != "*" ||
		value.left.left.kind != expressionInstanceVariable || value.left.left.text != "@trace" ||
		value.left.right.kind != expressionInteger || value.left.right.integer != 10 ||
		value.right.kind != expressionLocal || value.right.text != method.declaration.parameters[0] {
		return fmt.Errorf("ruby: mark body is outside prepared proof")
	}
	return nil
}

func validateAccessor(method *checkedMethod, field string) error {
	body := method.declaration.body
	if len(body) != 1 || body[0].kind != statementExpression || body[0].expression.kind != expressionInstanceVariable || body[0].expression.text != field {
		return fmt.Errorf("ruby: %s body is outside prepared proof", method.declaration.name)
	}
	return nil
}

func extractApply(method *checkedMethod) (int64, int64, int64, int64, error) {
	body := method.declaration.body
	if len(body) != 1 || body[0].kind != statementBegin {
		return 0, 0, 0, 0, fmt.Errorf("ruby: apply body is outside prepared proof")
	}
	begin := body[0]
	if len(begin.body) != 1 || begin.body[0].kind != statementExpression || begin.body[0].expression.kind != expressionYield ||
		len(begin.body[0].expression.args) != 1 || begin.body[0].expression.args[0].kind != expressionInstanceVariable || begin.body[0].expression.args[0].text != "@value" ||
		begin.rescueClass != "RuntimeError" || len(begin.rescueBody) != 2 || len(begin.ensureBody) != 2 {
		return 0, 0, 0, 0, fmt.Errorf("ruby: apply rescue/ensure shape is outside prepared proof")
	}
	rescueMark, ok := implicitIntegerCall(begin.rescueBody[0], "mark")
	if !ok {
		return 0, 0, 0, 0, fmt.Errorf("ruby: apply rescue mark is outside prepared proof")
	}
	rescueAdd, ok := instanceAddAssignment(begin.rescueBody[1], "@value")
	if !ok {
		return 0, 0, 0, 0, fmt.Errorf("ruby: apply rescue assignment is outside prepared proof")
	}
	ensureMark, ok := implicitIntegerCall(begin.ensureBody[0], "mark")
	if !ok {
		return 0, 0, 0, 0, fmt.Errorf("ruby: apply ensure mark is outside prepared proof")
	}
	ensureAdd, ok := instanceAddAssignment(begin.ensureBody[1], "@value")
	if !ok {
		return 0, 0, 0, 0, fmt.Errorf("ruby: apply ensure assignment is outside prepared proof")
	}
	return rescueMark, rescueAdd, ensureMark, ensureAdd, nil
}

func extractRunReturn(method *checkedMethod) (int64, int64, int64, error) {
	body := method.declaration.body
	if len(body) != 1 || body[0].kind != statementBegin || len(body[0].body) != 2 || len(body[0].ensureBody) != 1 {
		return 0, 0, 0, fmt.Errorf("ruby: run_return unwind shape is outside prepared proof")
	}
	call := body[0].body[0].expression
	if body[0].body[0].kind != statementExpression || !isReceiverCall(call, method.declaration.parameters[0], "apply", 0) || call.block == nil || len(call.block.parameters) != 1 || len(call.block.body) != 2 {
		return 0, 0, 0, fmt.Errorf("ruby: run_return block is outside prepared proof")
	}
	mark := call.block.body[0].expression
	if call.block.body[0].kind != statementExpression || !isReceiverCall(mark, method.declaration.parameters[0], "mark", 1) || mark.args[0].kind != expressionInteger {
		return 0, 0, 0, fmt.Errorf("ruby: run_return block mark is outside prepared proof")
	}
	returned := call.block.body[1]
	if returned.kind != statementReturn || returned.expression.kind != expressionBinary || returned.expression.text != "+" ||
		returned.expression.left.kind != expressionLocal || returned.expression.left.text != call.block.parameters[0] || returned.expression.right.kind != expressionInteger {
		return 0, 0, 0, fmt.Errorf("ruby: run_return return expression is outside prepared proof")
	}
	ensureMark, ok := receiverIntegerCall(body[0].ensureBody[0], method.declaration.parameters[0], "mark")
	if !ok {
		return 0, 0, 0, fmt.Errorf("ruby: run_return ensure is outside prepared proof")
	}
	return mark.args[0].integer, returned.expression.right.integer, ensureMark, nil
}

func validateReadLabel(method *checkedMethod) error {
	body := method.declaration.body
	if len(body) != 1 || len(method.declaration.parameters) != 1 || body[0].kind != statementExpression ||
		!isReceiverSymbolCall(body[0].expression, method.declaration.parameters[0], "send", "label") {
		return fmt.Errorf("ruby: read_label body is outside prepared proof")
	}
	return nil
}

func validateReadSaved(method *checkedMethod) error {
	body := method.declaration.body
	if len(body) != 1 || len(method.declaration.parameters) != 1 || body[0].kind != statementExpression || !isReceiverCall(body[0].expression, method.declaration.parameters[0], "saved_label", 0) {
		return fmt.Errorf("ruby: read_saved body is outside prepared proof")
	}
	return nil
}

func validateReadPublic(method *checkedMethod) (int64, error) {
	body := method.declaration.body
	if len(body) != 1 || len(method.declaration.parameters) != 1 || body[0].kind != statementBegin {
		return 0, fmt.Errorf("ruby: read_public body is outside prepared proof")
	}
	begin := body[0]
	if begin.rescueClass != "NoMethodError" || len(begin.body) != 1 || len(begin.rescueBody) != 1 || len(begin.ensureBody) != 0 ||
		begin.body[0].kind != statementExpression ||
		!isReceiverSymbolCall(begin.body[0].expression, method.declaration.parameters[0], "public_send", "label") ||
		begin.rescueBody[0].kind != statementExpression || begin.rescueBody[0].expression.kind != expressionInteger {
		return 0, fmt.Errorf("ruby: read_public rescue shape is outside prepared proof")
	}
	return begin.rescueBody[0].expression.integer, nil
}

func extractTopLevelValues(program *checkedProgram) (int64, int64, int64, int64, int64, string, error) {
	items := program.module.statements
	initial, ok := classNewInteger(items[8].expression, "Alpha")
	if !ok || !isReceiverCall(items[9].expression, "alpha", "equal?", 1) || items[9].expression.args[0].kind != expressionLocal || items[9].expression.args[0].text != "alpha" {
		return 0, 0, 0, 0, 0, "", fmt.Errorf("ruby: initial object/identity shape is outside prepared proof")
	}
	otherCall := items[10].expression
	if !isReceiverCall(otherCall, "alpha", "equal?", 1) {
		return 0, 0, 0, 0, 0, "", fmt.Errorf("ruby: other identity shape is outside prepared proof")
	}
	other, ok := classNewInteger(otherCall.args[0], "Alpha")
	beta, betaOK := classNewInteger(items[11].expression, "Beta")
	gamma, gammaOK := classNewInteger(items[12].expression, "Gamma")
	if !ok || !betaOK || !gammaOK ||
		!isImplicitCall(items[13].expression, "read_label", 1, "alpha") ||
		!isImplicitCall(items[14].expression, "read_label", 1, "beta") ||
		!isImplicitCall(items[15].expression, "read_label", 1, "gamma") ||
		!isImplicitCall(items[16].expression, "read_label", 1, "alpha") ||
		!isImplicitCall(items[17].expression, "read_label", 1, "beta") ||
		!isImplicitCall(items[19].expression, "read_label", 1, "alpha") ||
		!isImplicitCall(items[20].expression, "read_label", 1, "alpha") ||
		!isImplicitCall(items[21].expression, "read_label", 1, "beta") ||
		!isImplicitCall(items[22].expression, "read_label", 1, "gamma") ||
		!isImplicitCall(items[23].expression, "read_saved", 1, "alpha") ||
		!isImplicitCall(items[24].expression, "read_public", 1, "alpha") ||
		!isImplicitCall(items[26].expression, "read_public", 1, "alpha") ||
		!isImplicitCall(items[28].expression, "read_public", 1, "alpha") ||
		!isImplicitCall(items[29].expression, "read_label", 1, "alpha") ||
		!isImplicitCall(items[30].expression, "read_public", 1, "beta") ||
		!isImplicitCall(items[32].expression, "read_public", 1, "alpha") ||
		!isImplicitCall(items[34].expression, "read_label", 1, "beta") ||
		!isImplicitCall(items[36].expression, "run_return", 1, "alpha") {
		return 0, 0, 0, 0, 0, "", fmt.Errorf("ruby: top-level call order is outside prepared proof")
	}
	finalCall := items[37].expression
	if !isReceiverCall(finalCall, "alpha", "apply", 0) || finalCall.block == nil || len(finalCall.block.parameters) != 1 || len(finalCall.block.body) != 2 {
		return 0, 0, 0, 0, 0, "", fmt.Errorf("ruby: final apply block is outside prepared proof")
	}
	mark, ok := receiverIntegerCall(finalCall.block.body[0], "alpha", "mark")
	if !ok || finalCall.block.body[1].kind != statementRaise || finalCall.block.body[1].expression.kind != expressionString {
		return 0, 0, 0, 0, 0, "", fmt.Errorf("ruby: final raise block is outside prepared proof")
	}
	return initial, other, beta, gamma, mark, finalCall.block.body[1].expression.text, nil
}

func extractTopologyValues(program *checkedProgram) (int64, error) {
	items := program.module.statements
	if len(items) != 69 {
		return 0, fmt.Errorf("ruby: topology schedule is outside prepared proof")
	}
	alpha, alphaOK := classNewInteger(items[42].expression, "Alpha")
	beta, betaOK := classNewInteger(items[43].expression, "Beta")
	delta, deltaOK := classNewInteger(items[44].expression, "Delta")
	if !alphaOK || !betaOK || !deltaOK || alpha != beta || alpha != delta ||
		!isImplicitCall(items[45].expression, "read_label", 1, "c1_delta") ||
		!isImplicitCall(items[47].expression, "read_label", 1, "c1_delta") ||
		!isImplicitCall(items[49].expression, "read_label", 1, "c1_beta") ||
		!isImplicitCall(items[51].expression, "read_label", 1, "c1_beta") ||
		!isImplicitCall(items[53].expression, "read_label", 1, "c1_delta") ||
		!isImplicitCall(items[54].expression, "read_saved", 1, "c1_alpha") {
		return 0, fmt.Errorf("ruby: topology object/call schedule is outside prepared proof")
	}
	trace := items[55].expression
	if trace == nil || trace.kind != expressionBinary || trace.text != "+" ||
		trace.left == nil || trace.left.kind != expressionBinary || trace.left.text != "+" ||
		!receiverCallTimesInteger(trace.left.left, "c1_delta", "trace", 1000) ||
		!receiverCallTimesInteger(trace.left.right, "c1_beta", "trace", 10) ||
		!isReceiverCall(trace.right, "c1_alpha", "trace", 0) {
		return 0, fmt.Errorf("ruby: topology trace expression is outside prepared proof")
	}
	return alpha, nil
}

func extractSingletonValues(program *checkedProgram) (int64, error) {
	items := program.module.statements
	if len(items) != 69 || program.singletonAllocation != items[58] || program.singletonDefinitionCall != items[63].expression ||
		program.singletonOperation == nil || program.singletonOperation.id == 0 || program.singletonDefinition == nil {
		return 0, fmt.Errorf("ruby: singleton schedule is outside prepared proof")
	}
	receiver, receiverOK := classNewInteger(items[58].expression, "Alpha")
	peer, peerOK := classNewInteger(items[59].expression, "Alpha")
	if !receiverOK || !peerOK || receiver != peer ||
		!isImplicitCall(items[60].expression, "read_singleton", 1, "c2_receiver") ||
		!isImplicitCall(items[61].expression, "read_singleton", 1, "c2_receiver") ||
		!isImplicitCall(items[62].expression, "read_singleton", 1, "c2_peer") ||
		!isImplicitCall(items[64].expression, "read_singleton", 1, "c2_receiver") ||
		!isImplicitCall(items[65].expression, "read_singleton", 1, "c2_receiver") ||
		!isImplicitCall(items[66].expression, "read_singleton", 1, "c2_peer") {
		return 0, fmt.Errorf("ruby: singleton object/call schedule is outside prepared proof")
	}
	definitionCall := items[63].expression
	fact, ok := program.calls[definitionCall]
	if !ok || fact.kind != checkedCallDefineSingleton || fact.selector != program.singletonDefinition.selector ||
		fact.mutation != program.singletonOperation.id || fact.dispatch == 0 || fact.shape == 0 ||
		definitionCall.receiver == nil || definitionCall.receiver.kind != expressionLocal || definitionCall.receiver.text != "c2_receiver" ||
		len(definitionCall.args) != 1 || definitionCall.args[0].kind != expressionSymbol || definitionCall.args[0].text != "label" ||
		definitionCall.block == nil {
		return 0, fmt.Errorf("ruby: singleton definition call is outside prepared proof")
	}
	trace := items[67].expression
	if trace == nil || trace.kind != expressionBinary || trace.text != "+" ||
		!receiverCallTimesInteger(trace.left, "c2_receiver", "trace", 100) ||
		!isReceiverCall(trace.right, "c2_peer", "trace", 0) {
		return 0, fmt.Errorf("ruby: singleton trace expression is outside prepared proof")
	}
	return receiver, nil
}

func receiverCallTimesInteger(value *expression, receiver, name string, factor int64) bool {
	return value != nil && value.kind == expressionBinary && value.text == "*" &&
		isReceiverCall(value.left, receiver, name, 0) && value.right != nil && value.right.kind == expressionInteger && value.right.integer == factor
}

func implicitIntegerCall(item *statement, name string) (int64, bool) {
	if item.kind != statementExpression || item.expression.kind != expressionCall || item.expression.receiver != nil || item.expression.name != name || len(item.expression.args) != 1 || item.expression.args[0].kind != expressionInteger {
		return 0, false
	}
	return item.expression.args[0].integer, true
}

func receiverIntegerCall(item *statement, receiver, name string) (int64, bool) {
	if item.kind != statementExpression || !isReceiverCall(item.expression, receiver, name, 1) || item.expression.args[0].kind != expressionInteger {
		return 0, false
	}
	return item.expression.args[0].integer, true
}

func isReceiverCall(value *expression, receiver, name string, arity int) bool {
	return value != nil && value.kind == expressionCall && value.receiver != nil && value.receiver.kind == expressionLocal && value.receiver.text == receiver && value.name == name && len(value.args) == arity
}

func isReceiverSymbolCall(value *expression, receiver, name, symbol string) bool {
	return isReceiverCall(value, receiver, name, 1) && value.args[0].kind == expressionSymbol && value.args[0].text == symbol
}

func isImplicitCall(value *expression, name string, arity int, argument string) bool {
	return value != nil && value.kind == expressionCall && value.receiver == nil && value.name == name && len(value.args) == arity && arity == 1 && value.args[0].kind == expressionLocal && value.args[0].text == argument
}

func classNewInteger(value *expression, class string) (int64, bool) {
	if value == nil || value.kind != expressionCall || value.name != "new" || len(value.args) != 1 || value.args[0].kind != expressionInteger || value.receiver == nil || value.receiver.kind != expressionConstant || value.receiver.text != class {
		return 0, false
	}
	return value.args[0].integer, true
}

func isInstanceAssignment(item *statement, name string, kind expressionKind, text string) bool {
	return item.kind == statementAssign && item.name == name && item.expression.kind == kind && item.expression.text == text
}

func isInstanceIntegerAssignment(item *statement, name string, value int64) bool {
	return item.kind == statementAssign && item.name == name && item.expression.kind == expressionInteger && item.expression.integer == value
}

func instanceAddAssignment(item *statement, name string) (int64, bool) {
	if item.kind != statementAssign || item.name != name || item.expression.kind != expressionBinary || item.expression.text != "+" ||
		item.expression.left.kind != expressionInstanceVariable || item.expression.left.text != name || item.expression.right.kind != expressionInteger {
		return 0, false
	}
	return item.expression.right.integer, true
}
