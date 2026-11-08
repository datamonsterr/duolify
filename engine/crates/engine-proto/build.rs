fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto = "../../../proto/duolify/engine/v1/engine.proto";
    println!("cargo:rerun-if-changed={proto}");
    tonic_prost_build::configure()
        .file_descriptor_set_path(
            std::path::PathBuf::from(std::env::var("OUT_DIR")?).join("engine_descriptor.bin"),
        )
        .compile_protos(&[proto], &["../../../proto"])?;
    Ok(())
}
