use std::process::Command;

#[test]
fn offline_calibration_runs_and_reports_kappa() {
    let output = Command::new(env!("CARGO_BIN_EXE_duolify-eval"))
        .args(["judge-calibrate", "--mock"])
        .output()
        .unwrap();
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(
        stdout.contains("R8") && stdout.contains("kappa"),
        "{stdout}"
    );
}
