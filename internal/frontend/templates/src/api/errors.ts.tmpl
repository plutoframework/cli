export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId?: string;

  constructor(status: number, code: string, requestId?: string) {
    super(code);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

export function errorMessage(error: unknown): string {
  if (!(error instanceof ApiError))
    return "We could not reach Pluto. Check your connection and try again.";
  switch (error.status) {
    case 400:
      return "Check the values and try again. The server could not accept this request.";
    case 401:
      return "Your session or organization access needs attention. Sign in again or choose an organization you belong to.";
    case 403:
      return "This action is not available with your current permissions, feature settings, or resource policy.";
    case 404:
      return "This record is unavailable in the active organization.";
    case 409:
      return "This record changed, or this action has already been applied. Refresh before continuing.";
    case 429:
      return "Too many attempts. Wait a moment before trying again.";
    default:
      return "Pluto could not complete the request. Please try again later.";
  }
}
