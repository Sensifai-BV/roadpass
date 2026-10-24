import { lazy, Suspense, type ReactNode } from "react";
import { BrowserRouter, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { Shell } from "./components/Layout";
import { useMe } from "./lib/hooks";
import { canSee, type Area } from "./lib/access";
import { Skel } from "./components/ui";

const Landing = lazy(() => import("./pages/Landing"));
const Contact = lazy(() => import("./pages/Contact"));
const Login = lazy(() => import("./pages/Login"));
const Setup = lazy(() => import("./pages/Setup"));
const Join = lazy(() => import("./pages/Join"));
const Reset = lazy(() => import("./pages/Reset"));
const Overview = lazy(() => import("./pages/Overview"));
const Chain = lazy(() => import("./pages/Chain"));
const Passports = lazy(() => import("./pages/Passports"));
const PassportPage = lazy(() => import("./pages/Passport"));
const PublicPassport = lazy(() => import("./pages/PublicPassport"));
const Roads = lazy(() => import("./pages/Roads"));
const Quality = lazy(() => import("./pages/Quality"));
const Carbon = lazy(() => import("./pages/Carbon"));
const Plants = lazy(() => import("./pages/Plants"));
const Ledger = lazy(() => import("./pages/Ledger"));
const DataIn = lazy(() => import("./pages/DataIn"));
const Organisation = lazy(() => import("./pages/Organisation"));
const Settings = lazy(() => import("./pages/Settings"));
const Alerts = lazy(() => import("./pages/Alerts"));
const GetStarted = lazy(() => import("./pages/GetStarted"));
const Compliance = lazy(() => import("./pages/Compliance"));
const Platform = lazy(() => import("./pages/Platform"));
const Glossary = lazy(() => import("./pages/Glossary"));
const Explorer = lazy(() => import("./pages/Explorer"));
const Verify = lazy(() => import("./pages/Verify"));
const Work = lazy(() => import("./pages/Work"));
const Status = lazy(() => import("./pages/Status"));
const CalendarPage = lazy(() => import("./pages/Calendar"));

function Loading() {
  return (
    <div className="flex flex-col gap-4 p-2">
      <Skel h={34} w={280} />
      <Skel h={18} w={420} />
      <div className="mt-4 grid grid-cols-2 gap-4 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => <Skel key={i} h={132} />)}
      </div>
    </div>
  );
}

function RequireAuth({ children }: { children: ReactNode }) {
  const me = useMe();
  const loc = useLocation();
  if (me.isLoading) return <div className="p-10"><Loading /></div>;
  // Visitors who are not signed in see the public landing page at "/".
  if ((me.isError || !me.data) && loc.pathname === "/") return <Landing />;
  if (me.isError || !me.data) return <Navigate to={`/login?next=${encodeURIComponent(loc.pathname + loc.search)}`} replace />;
  return <>{children}</>;
}

/** The platform manager lands on the platform's dashboard; everyone else on their Overview. */
/** Sections a role does not use redirect to the Overview (see lib/access). */
function Allowed({ area, children }: { area: Area; children: ReactNode }) {
  const me = useMe().data;
  if (!me) return <Loading />;
  return canSee(area, me) ? <>{children}</> : <Navigate to="/" replace />;
}

function Home() {
  const me = useMe().data;
  return me?.operator && me.user.role === "admin" ? <Platform /> : <Overview />;
}

export function App() {
  return (
    <BrowserRouter>
      <Suspense fallback={<div className="p-10"><Loading /></div>}>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/setup" element={<Setup />} />
          <Route path="/join/:token" element={<Join />} />
          <Route path="/reset/:token" element={<Reset />} />
          <Route path="/p/:id" element={<PublicPassport />} />
          <Route path="/verify" element={<Verify />} />
          <Route path="/explorer/*" element={<Explorer />} />
          <Route path="/contact" element={<Contact />} />
          <Route element={<RequireAuth><Shell /></RequireAuth>}>
            <Route index element={<Suspense fallback={<Loading />}><Home /></Suspense>} />
            <Route path="chain" element={<Suspense fallback={<Loading />}><Chain /></Suspense>} />
            <Route path="work" element={<Suspense fallback={<Loading />}><Work /></Suspense>} />
            <Route path="status" element={<Suspense fallback={<Loading />}><Status /></Suspense>} />
            <Route path="calendar" element={<Suspense fallback={<Loading />}><CalendarPage /></Suspense>} />
            <Route path="passports" element={<Suspense fallback={<Loading />}><Passports /></Suspense>} />
            <Route path="passports/:id" element={<Suspense fallback={<Loading />}><PassportPage /></Suspense>} />
            <Route path="roads" element={<Suspense fallback={<Loading />}><Roads /></Suspense>} />
            <Route path="quality" element={<Suspense fallback={<Loading />}><Quality /></Suspense>} />
            <Route path="carbon" element={<Suspense fallback={<Loading />}><Carbon /></Suspense>} />
            <Route path="plants" element={<Suspense fallback={<Loading />}><Plants /></Suspense>} />
            <Route path="ledger" element={<Allowed area="ledger"><Suspense fallback={<Loading />}><Ledger /></Suspense></Allowed>} />
            <Route path="data" element={<Allowed area="data"><Suspense fallback={<Loading />}><DataIn /></Suspense></Allowed>} />
            <Route path="organisation" element={<Suspense fallback={<Loading />}><Organisation /></Suspense>} />
            <Route path="settings" element={<Suspense fallback={<Loading />}><Settings /></Suspense>} />
            <Route path="get-started" element={<Suspense fallback={<Loading />}><GetStarted /></Suspense>} />
            <Route path="compliance" element={<Allowed area="compliance"><Suspense fallback={<Loading />}><Compliance /></Suspense></Allowed>} />
            <Route path="alerts" element={<Allowed area="alerts"><Suspense fallback={<Loading />}><Alerts /></Suspense></Allowed>} />
            <Route path="dashboards/*" element={<Navigate to="/" replace />} />
            <Route path="platform" element={<Suspense fallback={<Loading />}><Platform /></Suspense>} />
            <Route path="platform/:tab" element={<Suspense fallback={<Loading />}><Platform /></Suspense>} />
            <Route path="admin" element={<Navigate to="/platform/organisations" replace />} />
            <Route path="glossary" element={<Suspense fallback={<Loading />}><Glossary /></Suspense>} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </Suspense>
    </BrowserRouter>
  );
}
