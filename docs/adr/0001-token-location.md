# Template

STATUS: accepted
DECISION: Store only token byte location inside the Token itself.

## Rationale
There is no need to store human readable locations directly on the token.
This would not add a lot of size to the token struct, however, it would impose complexity upon computation for zero reason.
For efficiency, it is better to provide a method to compute this on-demand so it can be calculated during debugging or when an error is reported.
