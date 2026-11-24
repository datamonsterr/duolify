# Landing Specification

## Purpose

Explain Duolify to a signed-out web visitor in one minute and lead them to sign-in.

## ADDED Requirements

### Requirement: Landing for signed-out web visitors

The web client SHALL show the landing page at `/` to a signed-out visitor and SHALL NOT show it to a signed-in Learner or in Tauri builds.

#### Scenario: Signed out on web

- **WHEN** a signed-out visitor opens `/` in a browser
- **THEN** the landing page shows the hero, video, pillars, steps, sign-in block and footer

#### Scenario: Signed in

- **WHEN** a signed-in Learner opens `/`
- **THEN** the Learn home shows and no landing content renders

### Requirement: Video does not autoplay or load eagerly

The landing video SHALL show a poster, SHALL NOT autoplay, SHALL use `preload="none"` and SHALL offer EN captions.

#### Scenario: Page load

- **WHEN** the landing loads
- **THEN** no video bytes are requested until the visitor presses play

### Requirement: One sign-in block

The sign-in block SHALL be a single centred column of at most 360 px with the age check, Google, Apple (only when SSO is enabled) and Get started at equal width, and SHALL NOT offer a second "I already have an account" button.

#### Scenario: Header sign-in

- **WHEN** the visitor presses Sign in in the top bar
- **THEN** the guided sign-in chat opens in returning mode

### Requirement: Footer privacy note

The footer SHALL link Privacy, Terms and Analytics preference and SHALL state only privacy facts the product already enforces.

#### Scenario: Footer is displayed

- **WHEN** a visitor reaches the footer
- **THEN** the privacy and terms links, analytics preference and privacy note are available

### Requirement: Anonymous landing analytics

The client SHALL send `landing_viewed`, `landing_video_played`, `landing_video_completed` and `landing_cta_clicked{target}` only when the visitor has not opted out, with no identity and no free text.

#### Scenario: Opted out

- **WHEN** the visitor opted out of analytics
- **THEN** no landing event is captured
