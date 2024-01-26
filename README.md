## kitchen

A collection of convenience functions, structs, and interfaces that I wrote to simplify their implementation into existing codebases
# Currently included
* Pooler

  `stage: need to finish documentation, add integration tests/benchmarks/example implementations, test backwards compatibility`
  - Wrapper for typed instances of sync.Pool, since sync.Pool is not currently adapted for generics in the standard library.
  - Poolers handle the type assertion and object resetting issues native to sync.Pool on your behalf.
  - Also included is the ability to create a Recycler from a Pooler. Recyclers wrap values from their corresponding Poolers in an interface that enables returning the value to its origin pool without requiring your code paths to contain a direct reference to said pool/Pooler. 

