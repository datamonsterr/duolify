use super::*;

#[test]
fn kappa_handles_agreement_disagreement_and_degenerate_labels() {
    assert_eq!(
        kappa(&[true, true, false, false], &[true, true, false, false]),
        Some(1.0)
    );
    assert_eq!(
        kappa(&[true, true, false, false], &[false, false, true, true]),
        Some(-1.0)
    );
    assert_eq!(kappa(&[true, true], &[true, true]), None);
}
