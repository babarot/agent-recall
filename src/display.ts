/** Format a project path for display: prefer project_path, shorten $HOME to ~ */
export function displayProject(projectPath: string, project: string): string {
  const path = projectPath || project.replace(/^-/, "/").replaceAll("-", "/");
  const home = Deno.env.get("HOME");
  if (home && path.startsWith(home)) {
    return "~" + path.slice(home.length);
  }
  return path;
}
