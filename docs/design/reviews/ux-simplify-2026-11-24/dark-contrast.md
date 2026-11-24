# Dark contrast: initial findings

Ratios are WCAG relative-luminance calculations from the resolved dark theme token values, rounded to two decimal places. They identify the color pairs in the stylesheet; an automated route scan is still needed to confirm where each state is rendered.

| Surface | Before | After | Minimum |
| --- | ---: | ---: | ---: |
| Active transcript, text on highlight | 1.07:1 (#f1f7f2 on #ddf1ff) | 8.36:1 (#9fd3ff on #173247) | 4.5:1 |
| Narration hint / locked voice on surface | 2.15:1 (#5b5b5b on #1c2b31) | 8.37:1 (#b8c7c9 on #1c2b31) | 4.5:1 |
| Narration highlighted caption | 1.02:1 (#f1f7f2 on #d7ffb8) | 8.04:1 (#9be07a on #1a3a12) | 4.5:1 |
| Bookmarks heading / empty label | 1.18:1 (#2b2b2b on #131f24) | 15.47:1 (#f1f7f2 on #131f24) | 4.5:1 |
| Sessions primary button during initial transition | 1.10:1 (#0e2606 on #1c2b31) | 8.56:1 (#0e2606 on #6cd43a) | 4.5:1 |
| Social danger button | 2.02:1 (#fff on #ff9b95) | 6.94:1 (#4a1e1e on #ff9b95) | 4.5:1 |
| Lesson test exit link | 2.85:1 (#1667ab on #131f24) | 10.60:1 (#9fd3ff on #131f24) | 4.5:1 |
| Lesson test header | 2.92:1 (#666 on #131f24) | 9.65:1 (#b8c7c9 on #131f24) | 4.5:1 |
| Lesson test question | 1.18:1 (#2b2b2b on #131f24) | 15.47:1 (#f1f7f2 on #131f24) | 3:1 |
| Lesson test error body | 1.52:1 (#b8c7c9 on #fdecec) | 6.94:1 (#ff9b95 on #4a1e1e) | 4.5:1 |
| Lesson test disabled Check label | 2.26:1 (#999 on #e5e5e5) | 8.37:1 (#b8c7c9 on #1c2b31) | 4.5:1 |
| Lesson test selection hint | 2.59:1 (#5e5e5e on #131f24) | 9.65:1 (#b8c7c9 on #131f24) | 4.5:1 |
| Shared checkbox, disabled outline on surface | 2.57:1 (60% #7f9499 on #1c2b31) | 4.58:1 (#7f9499 on #1c2b31) | 3:1 |
| Shared checkbox focus ring on surface | 7.78:1 (#7cc4ff on #1c2b31) | 7.78:1 | 3:1 |
| Tutor disabled icon border on canvas | 2.33:1 (50% #7f9499 on #131f24) | 5.28:1 (#7f9499 on #131f24) | 3:1 |

The initial axe scan found Bookmarks and sessions failures shown above. After waiting for session loading and disabling motion, the scan passes. The sessions result occurred during the button transition; intermediate animation colors remain unmeasured. The Quick notes route had multiple failures and is excluded by request.

The focused browser scan passed 19 initial authenticated routes plus a populated lesson transcript, lesson test error, course publication preview, marketplace report dialog, populated Course Cast, friend invite and blocked course, public preview, SSO confirmation and error, and sign-in welcome. The initial route scans can include loading or empty states and are not proof that all content states pass.

Deferred / unaudited: populated social, character, course and bookmark states; other dialogs and errors; RTL, mobile and animation intermediate colors. Slides inside the sandboxed lesson frame and Quick notes are outside the scan. These gaps prevent a claim that every requested route meets AA. The ratios assume the stated background and do not substitute for checking rendered states, transparency or images.
