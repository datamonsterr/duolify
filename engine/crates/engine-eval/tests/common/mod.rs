//! Locate the `duolify-eval` binary under `cargo test` and `cargo nextest`.

/// `env!("CARGO_BIN_EXE_*")` bakes in the build-time absolute path, which does
/// not exist when a nextest archive is run elsewhere. Nextest exports the
/// remapped path at runtime, so prefer it and fall back to the compile-time
/// path for plain `cargo test`.
pub fn eval_bin() -> &'static str {
    match std::env::var("NEXTEST_BIN_EXE_duolify_eval") {
        Ok(path) => Box::leak(path.into_boxed_str()),
        Err(_) => env!("CARGO_BIN_EXE_duolify-eval"),
    }
}
