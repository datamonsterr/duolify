## ADDED Requirements

### Requirement: Original mascot brand exception
The brand rule against mascots SHALL be amended: Duolify has one original default mascot (see the `mascot` capability) and MUST still avoid owl or bird silhouettes, Duolingo's name, logo, fonts or copy and any borrowed character. Mascot colours MUST come from tokens. `docs/design/brand.md` MUST state this exception.

#### Scenario: Brand review
- **WHEN** a new mascot pose or accessory is added
- **THEN** it is reviewed against the originality rule and the token list before release

### Requirement: Chat widget accessibility contract
Guided chat widgets MUST use semantic form controls, token colours and the existing focus ring, MUST support light and dark, RTL mirroring, reduced motion and 200% text size, and design review MUST capture each script at 390x844, 844x390 and 1280x800.

#### Scenario: Landscape phone
- **WHEN** a guided chat is shown at 844x390
- **THEN** the widget and composer stay reachable without clipping and the mascot shrinks or hides before covering them
