class GolfTrackError(Exception):
    pass


class NotFoundError(GolfTrackError):
    pass


class ConflictError(GolfTrackError):
    pass


class ValidationError(GolfTrackError):
    pass


class UnauthorizedError(GolfTrackError):
    pass


class ForbiddenError(GolfTrackError):
    pass
