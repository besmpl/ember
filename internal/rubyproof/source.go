package rubyproof

// ProofSource is the bounded Ruby program used by the architecture proof. It
// is ordinary Ruby 2.6 syntax and is also executed by the pinned CRuby oracle.
// Base/Alpha/Beta/Gamma/Delta and the two label modules deliberately exercise
// inherited lookup, shadowed ancestor redefinition, alias snapshotting,
// remove-versus-undef entry state, include/prepend topology changes,
// visibility-aware send/public_send legality, one allocation-site singleton
// method, and bounded call sites while retaining the original
// block/return/rescue/ensure and object-state proof.
const ProofSource = `class Base
  def initialize(value)
    @value = value
    @trace = 0
  end

  def mark(digit)
    @trace = @trace * 10 + digit
  end

  def label()
    mark(6)
    1
  end

  alias_method(:saved_label, :label)

  def hot()
    @value
  end

  def apply()
    begin
      yield(@value)
    rescue RuntimeError
      mark(3)
      @value = @value + 100
    ensure
      mark(2)
      @value = @value + 1
    end
  end

  def value()
    @value
  end

  def trace()
    @trace
  end
end

class Alpha < Base
end

class Beta < Base
  def initialize(value)
    @beta = value
    @value = value
    @trace = 0
  end

  def label()
    mark(8)
    3
  end
end

class Gamma < Base
  def initialize(value)
    @trace = 0
    @gamma = value
    @value = value
  end

  def label()
    mark(9)
    4
  end
end

def read_label(receiver)
  receiver.send(:label)
end

def read_saved(receiver)
  receiver.saved_label()
end

def read_public(receiver)
  begin
    receiver.public_send(:label)
  rescue NoMethodError
    5
  end
end

def run_return(receiver)
  receiver.apply() do |value|
    receiver.mark(1)
    return(value + 10)
  end
  999
ensure
  receiver.mark(4)
end

alpha = Alpha.new(4)
same = alpha.equal?(alpha)
other = alpha.equal?(Alpha.new(4))
beta = Beta.new(4)
gamma = Gamma.new(4)
before = read_label(alpha)
beta_before = read_label(beta)
gamma_before = read_label(gamma)
alpha_warm = read_label(alpha)
beta_warm = read_label(beta)

class Base
  def label()
    mark(7)
    2
  end
end

after = read_label(alpha)
alpha_after_hit = read_label(alpha)
beta_after = read_label(beta)
gamma_after = read_label(gamma)
saved = read_saved(alpha)
public_before = read_public(alpha)

class Base
  protected(:label)
end

protected_rejected = read_public(alpha)

class Base
  private(:label)
end

private_rejected = read_public(alpha)
private_sent = read_label(alpha)
private_beta = read_public(beta)

class Base
  public(:label)
end

public_restored = read_public(alpha)

class Beta
  remove_method(:label)
end

beta_removed = read_label(beta)

class Gamma
  undef_method(:label)
end

returned = run_return(alpha)
alpha.apply() do |value|
  alpha.mark(5)
  raise("boom")
end
result = [same, other, before, beta_before, gamma_before, alpha_warm, beta_warm, after, alpha_after_hit, beta_after, gamma_after, saved, public_before, protected_rejected, private_rejected, private_sent, private_beta, public_restored, beta_removed, returned, alpha.value(), alpha.trace(), beta.trace(), gamma.trace()]

module IncludedLabel
  def label()
    mark(1)
    11
  end
end

module PrependedLabel
end

class Delta < Alpha
end

c1_alpha = Alpha.new(10)
c1_beta = Beta.new(10)
c1_delta = Delta.new(10)
c1_delta_before = read_label(c1_delta)

class Alpha
  include(IncludedLabel)
end

c1_delta_included = read_label(c1_delta)

class Beta
  prepend(PrependedLabel)
end

c1_beta_route = read_label(c1_beta)

module PrependedLabel
  def label()
    mark(2)
    22
  end
end

c1_beta_defined = read_label(c1_beta)

module IncludedLabel
  def label()
    mark(3)
    13
  end
end

c1_delta_redefined = read_label(c1_delta)
c1_saved_stable = read_saved(c1_alpha)
c1_trace = c1_delta.trace() * 1000 + c1_beta.trace() * 10 + c1_alpha.trace()
c1_result = [c1_delta_before, c1_delta_included, c1_beta_route, c1_beta_defined, c1_delta_redefined, c1_saved_stable, c1_trace]

def read_singleton(receiver)
  receiver.send(:label)
end

c2_receiver = Alpha.new(10)
c2_peer = Alpha.new(10)
c2_before = read_singleton(c2_receiver)
c2_warm = read_singleton(c2_receiver)
c2_peer_before = read_singleton(c2_peer)
c2_receiver.define_singleton_method(:label) do
  mark(4)
  44
end
c2_after = read_singleton(c2_receiver)
c2_after_hit = read_singleton(c2_receiver)
c2_peer_after = read_singleton(c2_peer)
c2_trace = c2_receiver.trace() * 100 + c2_peer.trace()
c2_result = [c2_before, c2_warm, c2_peer_before, c2_after, c2_after_hit, c2_peer_after, c2_trace]
`

const ProofOracleOutput = "true|false|1|3|4|1|3|2|2|3|4|1|2|5|5|2|3|2|2|14|106|66776777124532|88887|99"

const ProofC1OracleOutput = "2|11|2|22|13|1|713726"

const ProofC2OracleOutput = "13|13|13|44|44|13|334433"
