import {
  Link as RouterLink,
  Navigate as RouterNavigate,
  NavLink as RouterNavLink,
  useParams,
  type LinkProps,
  type NavigateProps,
  type NavLinkProps,
} from "react-router";

import { inCluster } from "./clusterPaths";

// React Router's Link, NavLink and Navigate, on the cluster the page is on:
// to="/workloads/pods" goes to /c/<cluster>/workloads/pods (see clusterPaths).

export function Link(props: LinkProps) {
  const { cluster } = useParams();
  return <RouterLink {...props} to={inCluster(props.to, cluster)} />;
}

export function NavLink(props: NavLinkProps) {
  const { cluster } = useParams();
  return <RouterNavLink {...props} to={inCluster(props.to, cluster)} />;
}

export function Navigate(props: NavigateProps) {
  const { cluster } = useParams();
  return <RouterNavigate {...props} to={inCluster(props.to, cluster)} />;
}
