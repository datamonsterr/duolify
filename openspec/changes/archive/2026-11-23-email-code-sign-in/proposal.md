# Email-code sign-in (E1-02, #41)

Replace scaffold entry with first-party email one-time codes, an authenticated
Learner/profile read, and device credentials. Resend is behind a Mailer interface;
development and tests use a local mail sink. OAuth and onboarding remain separate tickets.

The welcome screen follows S01 with email-code steps, recovery controls and safe
internal return paths. Analytics use UUID identity only after verification.
