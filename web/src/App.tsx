import { useMemo, type ReactElement } from "react";
import { useTranslation } from "react-i18next";
import { createRootRoute, createRoute, createRouter, RouterProvider, useNavigate } from "@tanstack/react-router";
import { Layout } from "./components/Layout";
import { Dashboard } from "./pages/Pages";
import { CommandsPage } from "./pages/CommandsPage";
import { CommandEditor } from "./pages/CommandEditor";
import { ServersPage } from "./pages/ServersPage";
import { IdentitiesPage } from "./pages/IdentitiesPage";
import { RulesPage } from "./pages/RulesPage";
import { KeysPage } from "./pages/KeysPage";
import { SimulatorPage } from "./pages/SimulatorPage";
import { ApprovalsPage } from "./pages/ApprovalsPage";
import { JournalView } from "./pages/JournalPage";
import { PacksPage } from "./pages/PacksPage";
import { ProvisioningPage } from "./pages/ProvisioningPage";
import { AuditPage } from "./pages/AuditPage";
import { ProposalDetail, ProposalEditorRoute, ProposalsPage } from "./pages/ProposalsPage";
import { SettingsPage } from "./pages/SettingsPage";
import { parseSearch, type JournalSearch } from "./lib/journal";
import { LoginPage, SetupPage } from "./pages/AuthPages";
import { useMe, useSessionActions, useSetupStatus } from "./lib/useAuth";
import type { User } from "./lib/api";


function buildRouter(user: User, onLogout: () => void) {
  const root = createRootRoute({ component: () => <Layout user={user} onLogout={onLogout} /> });
  const index = createRoute({ getParentRoute: () => root, path: "/", component: Dashboard });
  const page = (path: string, component: () => ReactElement) => createRoute({ getParentRoute: () => root, path, component });
  const cmdEdit = createRoute({ getParentRoute: () => root, path: "/commandes/$id", component: function Edit() { return <CommandEditor id={cmdEdit.useParams().id} />; } });
  const journal = createRoute({
    getParentRoute: () => root, path: "/journal",
    validateSearch: (s: Record<string, unknown>) => parseSearch(s),
    component: function Journal() {
      const search = journal.useSearch() as JournalSearch;
      const navigate = useNavigate({ from: "/journal" });
      return <JournalView search={search} onSearch={(next) => void navigate({ search: next as never })} />;
    },
  });
  const propDetail = createRoute({ getParentRoute: () => root, path: "/propositions/$id", component: function PD() { return <ProposalDetail id={propDetail.useParams().id} />; } });
  const propEdit = createRoute({ getParentRoute: () => root, path: "/propositions/$id/modifier", component: function PE() { return <ProposalEditorRoute id={propEdit.useParams().id} />; } });
  const routes = [journal, page("/packs", PacksPage), page("/propositions", ProposalsPage), propDetail, propEdit, page("/reglages", SettingsPage),
    page("/commandes", CommandsPage), page("/commandes/nouvelle", () => <CommandEditor />), cmdEdit,
    page("/serveurs", ServersPage), page("/provisioning", ProvisioningPage), page("/audit", AuditPage), page("/identites", IdentitiesPage), page("/regles", RulesPage),
    page("/cles", KeysPage), page("/simulateur", SimulatorPage), page("/validations", ApprovalsPage),
  ];
  return createRouter({ routeTree: root.addChildren([index, ...routes]) });
}

function Authenticated({ user, onLogout }: { user: User; onLogout: () => void }) {
  const router = useMemo(() => buildRouter(user, onLogout), [user, onLogout]);
  return <RouterProvider router={router} />;
}

export default function App() {
  const { t } = useTranslation();
  const setup = useSetupStatus();
  const me = useMe(setup.data?.initialized === true);
  const { onAuthenticated, logout } = useSessionActions();

  if (setup.isError || me.isError) return <p role="alert" className="p-8 text-destructive">{t("errors.generic")}</p>;
  if (setup.isPending || (setup.data.initialized && me.isPending)) return <p className="p-8 text-muted">{t("errors.loading")}</p>;
  if (!setup.data.initialized) return <SetupPage onDone={onAuthenticated} />;
  if (!me.data) return <LoginPage onDone={onAuthenticated} />;
  return <Authenticated user={me.data} onLogout={() => void logout()} />;
}
