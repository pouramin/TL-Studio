// Project and session selection are independent asynchronous ownership domains.
// Responses started for an older selection must never mutate the current view.
// Tokens are in-memory only: they are not persisted and never affect backend runs.
export const createViewScope = () => {
  let projectEpoch = 0;
  let sessionEpoch = 0;

  const enterProject = () => {
    projectEpoch += 1;
    sessionEpoch += 1;
  };
  const enterSession = () => {
    sessionEpoch += 1;
  };
  const captureProject = () => projectEpoch;
  const captureSession = () => ({ project: projectEpoch, session: sessionEpoch });
  const isProjectCurrent = (token: number) => token === projectEpoch;
  const isSessionCurrent = (token: { project: number; session: number }) =>
    !!token && token.project === projectEpoch && token.session === sessionEpoch;

  return { enterProject, enterSession, captureProject, captureSession, isProjectCurrent, isSessionCurrent };
};
