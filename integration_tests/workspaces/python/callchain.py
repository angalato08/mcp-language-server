"""Call chain module for testing call hierarchy."""


def leaf_function() -> str:
    """A leaf function with no outgoing calls."""
    return "leaf result"


def middle_function() -> str:
    """A function that calls leaf_function."""
    result = leaf_function()
    return f"middle: {result}"


def entry_point() -> None:
    """An entry point that calls middle_function."""
    msg = middle_function()
    print(msg)


def another_caller() -> None:
    """Another caller of middle_function."""
    print(middle_function())
