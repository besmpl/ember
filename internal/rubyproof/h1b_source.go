package rubyproof

// H1bSource is the exact bounded Ruby program used to prove one captured,
// receiver-specific singleton method with explicit guest collection points.
// It deliberately does not claim general closures, singleton classes, or GC.
const H1bSource = `class H1bReceiver
  def label()
    7
  end
end

def read_h1b(receiver)
  receiver.label()
end

def install_h1b(receiver)
  captured = 40
  receiver.define_singleton_method(:label) do
    captured = captured + 1
  end
  GC.start()
  captured = captured + 2
  receiver
end

receiver = H1bReceiver.new()
peer = H1bReceiver.new()
before = read_h1b(receiver)
warm = read_h1b(receiver)
peer_before = read_h1b(peer)
receiver = install_h1b(receiver)
first = read_h1b(receiver)
second = read_h1b(receiver)
peer_after = read_h1b(peer)
receiver = 0
GC.start()
peer_after_receiver_collection = read_h1b(peer)
h1b_result = [before, warm, peer_before, first, second, peer_after, peer_after_receiver_collection]
peer = 0
GC.start()
`

// H1bOracleOutput is the detached scalar line produced by H1bSource. The
// trailing newline belongs to the application projection rather than Ruby's
// result value and is therefore omitted here, as it is for the other proofs.
const H1bOracleOutput = "7|7|7|43|44|7|7"
