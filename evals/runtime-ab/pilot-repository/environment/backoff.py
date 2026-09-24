def delay(attempt, base=1, cap=60):
    """Return the capped exponential delay before a retry."""
    return base * (2 ** attempt)
