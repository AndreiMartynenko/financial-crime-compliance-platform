import React, {useEffect, useState} from 'react'
import {createRoot} from 'react-dom/client'
import {UserManager, WebStorageStateStore} from 'oidc-client-ts'
import './styles.css'

const api = async (path, token, options = {}) => {
  const response = await fetch('/api' + path, {
    ...options,
    headers: {'Content-Type': 'application/json', Authorization: `Bearer ${token}`, ...options.headers},
  })
  const data = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(data?.error?.message || `Request failed (${response.status})`)
  return data
}
const oidc = new UserManager({
  authority: window.__FCCP_CONFIG__.oidcAuthority,
  client_id: window.__FCCP_CONFIG__.oidcClientId,
  redirect_uri: window.location.origin + '/',
  post_logout_redirect_uri: window.location.origin + '/',
  response_type: 'code',
  scope: 'openid profile email',
  userStore: new WebStorageStateStore({store: sessionStorage}),
})
const badge = value => <span className={`badge ${value}`}>{String(value).replaceAll('_', ' ')}</span>
const operatorRole = claims => claims.realm_access?.roles?.find(role => ['analyst', 'reviewer', 'admin'].includes(role))
const tokenClaims = token => {
  try {
    const encoded = token.split('.')[1].replaceAll('-', '+').replaceAll('_', '/')
    return JSON.parse(atob(encoded.padEnd(Math.ceil(encoded.length / 4) * 4, '=')))
  } catch { return {} }
}

function Login({onLogin, error}) {
  return <main className="login"><section className="login-card"><div className="brand-mark">FC</div><p className="eyebrow">Secure operations portal</p><h1>Financial Crime<br/>Compliance Platform</h1><p className="muted">Sign in through the organization identity provider using Authorization Code and PKCE.</p>{error && <div className="error">{error}</div>}<button onClick={onLogin}>Sign in with Keycloak</button><p className="fine">Credentials are handled only by the identity provider. The portal never receives your password.</p></section></main>
}

function App() {
  const [user, setUser] = useState(null)
  const [authReady, setAuthReady] = useState(false)
  const [view, setView] = useState('dashboard')
  const [customers, setCustomers] = useState([])
  const [alerts, setAlerts] = useState([])
  const [cases, setCases] = useState([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const token = user?.access_token
  const claims = user?.profile || {}
  const role = operatorRole(tokenClaims(token || ''))

  useEffect(() => { (async () => {
    try {
      if (new URLSearchParams(location.search).has('code')) {
        await oidc.signinRedirectCallback()
        history.replaceState({}, '', location.pathname)
      }
      setUser(await oidc.getUser())
    } catch (e) { setError(e.message) } finally { setAuthReady(true) }
  })() }, [])
  const load = async () => {
    if (!token) return
    setLoading(true); setError('')
    try {
      const [customerPage, alertPage, casePage] = await Promise.all([
        api('/v1/customers?page_size=100', token), api('/v1/alerts?page_size=100', token), api('/v1/cases?page_size=100', token),
      ])
      setCustomers(customerPage.items || []); setAlerts(alertPage.items || []); setCases(casePage.items || [])
    } catch (e) { setError(e.message) } finally { setLoading(false) }
  }
  useEffect(() => { load() }, [token])
  if (!authReady) return <div className="loading">Establishing secure session…</div>
  if (!user || user.expired) return <Login onLogin={() => oidc.signinRedirect()} error={error}/>

  const pending = customers.filter(customer => customer.status === 'pending_approval')
  const openAlerts = alerts.filter(alert => alert.status === 'open')
  const openCases = cases.filter(item => item.status !== 'resolved')
  const nav = [['dashboard', 'Overview'], ['customers', 'Customers'], ['cdd', 'KYC / CDD'], ['screening', 'Screening'], ['approvals', `Approvals ${pending.length}`], ['alerts', `Alerts ${openAlerts.length}`], ['cases', `Cases ${openCases.length}`]]
  return <div className="shell"><aside><div className="brand"><div className="brand-mark small">FC</div><div><strong>Northstar</strong><span>Compliance OS</span></div></div><nav>{nav.map(([id, label]) => <button className={view === id ? 'active' : ''} onClick={() => setView(id)} key={id}>{label}</button>)}</nav><div className="profile"><div className="avatar">{(claims.preferred_username || claims.sub)?.[0]?.toUpperCase()}</div><div><strong>{claims.preferred_username || claims.sub}</strong><span>{role}</span></div><button aria-label="Sign out" onClick={() => oidc.signoutRedirect()}>↗</button></div></aside><main className="workspace"><header><div><p className="eyebrow">Operations / {view}</p><h1>{view[0].toUpperCase() + view.slice(1)}</h1></div><div className="header-actions"><span className="live">● Systems operational</span><button className="secondary" onClick={load}>Refresh</button></div></header>{error && <div className="error">{error}</div>}{loading ? <div className="loading">Loading verified compliance data…</div> : <Content view={view} customers={customers} alerts={alerts} cases={cases} token={token} role={role} reload={load}/>}</main></div>
}

function Content({view, customers, alerts, cases, token, role, reload}) {
  if (view === 'dashboard') return <Dashboard customers={customers} alerts={alerts}/>
  if (view === 'customers') return <Customers items={customers} alerts={alerts} cases={cases} token={token} role={role} reload={reload}/>
  if (view === 'cdd') return <CDDWorkspace customers={customers} token={token} role={role}/>
  if (view === 'screening') return <ScreeningWorkspace customers={customers} token={token} role={role}/>
  if (view === 'approvals') return <Approvals items={customers.filter(customer => customer.status === 'pending_approval')} token={token} role={role} reload={reload}/>
  if (view === 'alerts') return <Alerts items={alerts} cases={cases} token={token} role={role} reload={reload}/>
  return <Cases items={cases} token={token} role={role} reload={reload}/>
}
function Dashboard({customers, alerts}) {
  const cards = [['Total customers', customers.length, 'All monitored entities'], ['Pending approval', customers.filter(c => c.status === 'pending_approval').length, 'Requires independent review'], ['Open alerts', alerts.filter(a => a.status === 'open').length, 'Investigation queue'], ['High severity', alerts.filter(a => a.status === 'open' && a.severity === 'high').length, 'Immediate attention']]
  return <><section className="metrics">{cards.map(([label, value, summary]) => <article key={label}><span>{label}</span><strong>{value}</strong><small>{summary}</small></article>)}</section><section className="panel"><div className="panel-title"><div><p className="eyebrow">Live queue</p><h2>Recent monitoring alerts</h2></div></div><AlertTable items={alerts.slice(0, 6)}/></section></>
}

function Customers({items, alerts, cases, token, role, reload}) {
  const [mode, setMode] = useState('list')
  const [selected, setSelected] = useState(null)
  const canWrite = role === 'analyst' || role === 'admin'
  const completed = async () => { setMode('list'); setSelected(null); await reload() }
  if (mode === 'onboard') return <CustomerForm token={token} onCancel={() => setMode('list')} onComplete={completed}/>
  if (mode === 'transaction') return <TransactionForm token={token} customers={items.filter(c => c.status === 'active')} initialCustomer={selected} onCancel={() => setMode('list')} onComplete={reload}/>
  if (mode === 'detail') return <Customer360 customer={selected} alerts={alerts.filter(item => item.customer_id === selected.id)} cases={cases.filter(item => item.customer_id === selected.id)} token={token} onBack={() => setMode('list')} onTransaction={selected.status === 'active' && canWrite ? () => setMode('transaction') : null}/>
  return <section className="panel"><div className="panel-title"><div><p className="eyebrow">Customer lifecycle</p><h2>Risk-rated entities</h2></div><div className="panel-actions"><span>{items.length} records</span>{canWrite && <><button className="secondary" disabled={!items.some(c => c.status === 'active')} onClick={() => { setSelected(null); setMode('transaction') }}>Add transaction</button><button onClick={() => setMode('onboard')}>New customer</button></>}</div></div>{items.length === 0 ? <Empty text="No customers registered" detail="Create the first customer to start the compliance lifecycle."/> : <div className="table-wrap"><table><thead><tr><th>Customer</th><th>Country</th><th>Risk</th><th>Due diligence</th><th>Status</th><th>Created</th><th></th></tr></thead><tbody>{items.map(customer => <tr key={customer.id}><td><strong>{customer.legal_name}</strong><small>{customer.external_ref || customer.id.slice(0, 8)}</small></td><td>{customer.country_code}</td><td>{badge(customer.risk_assessment.rating)}</td><td>{customer.risk_assessment.due_diligence.replaceAll('_', ' ')}</td><td>{badge(customer.status)}</td><td>{new Date(customer.created_at).toLocaleDateString()}</td><td><div className="row-actions"><button className="compact" onClick={() => { setSelected(customer); setMode('detail') }}>View 360°</button>{canWrite && customer.status === 'active' && <button className="compact" onClick={() => { setSelected(customer); setMode('transaction') }}>Transaction</button>}</div></td></tr>)}</tbody></table></div>}</section>
}

function Customer360({customer, alerts, cases, token, onBack, onTransaction}) {
  const [transactions, setTransactions] = useState([])
  const [activity, setActivity] = useState([])
  const [filter, setFilter] = useState('all')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  useEffect(() => { (async () => {
    try {
      const [transactionPage, activityPage] = await Promise.all([
        api(`/v1/customers/${customer.id}/transactions?page_size=100`, token),
        api(`/v1/customers/${customer.id}/activity?page_size=100`, token),
      ])
      setTransactions(transactionPage.items || []); setActivity(activityPage.items || [])
    } catch (e) { setError(e.message) } finally { setLoading(false) }
  })() }, [customer.id, token])
  const factors = Object.entries(customer.risk_factors).filter(([key, value]) => key !== 'source_of_funds_verified' && value === true).map(([key]) => key.replaceAll('_', ' '))
  if (!customer.risk_factors.source_of_funds_verified) factors.push('source of funds unverified')
  const visibleActivity = filter === 'all' ? activity : activity.filter(event => event.aggregate_type === filter)
  const formatAmount = transaction => new Intl.NumberFormat(undefined, {style: 'currency', currency: transaction.currency}).format(transaction.amount_minor / 100)
  return <section className="customer360"><div className="detail-toolbar"><button className="secondary" onClick={onBack}>← All customers</button>{onTransaction && <button onClick={onTransaction}>Add transaction</button>}</div><div className="customer-hero panel"><div><p className="eyebrow">Customer 360</p><h2>{customer.legal_name}</h2><p>{customer.type} · {customer.country_code} · {customer.external_ref || customer.id}</p></div><div>{badge(customer.risk_assessment.rating)} {badge(customer.status)}</div></div>{error && <div className="error">{error}</div>}{loading ? <div className="loading">Building customer intelligence view…</div> : <><section className="customer-metrics"><article><span>Risk score</span><strong>{customer.risk_assessment.score}</strong><small>{customer.risk_assessment.rule_version}</small></article><article><span>Transactions</span><strong>{transactions.length}</strong><small>Recorded activity</small></article><article><span>Alerts</span><strong>{alerts.length}</strong><small>{alerts.filter(item => item.status === 'open').length} open</small></article><article><span>Cases</span><strong>{cases.length}</strong><small>{cases.filter(item => item.status !== 'resolved').length} active</small></article></section><div className="customer-columns"><section className="panel"><div className="panel-title"><div><p className="eyebrow">Explainable scoring</p><h2>Risk assessment</h2></div><span>{customer.risk_assessment.due_diligence.replaceAll('_', ' ')}</span></div><div className="factor-tags">{factors.length ? factors.map(item => <span key={item}>{item}</span>) : <span>No elevated factors</span>}</div><div className="risk-reasons">{customer.risk_assessment.reasons.length ? customer.risk_assessment.reasons.map(reason => <article key={reason.code}><strong>+{reason.points}</strong><div><h3>{reason.code.replaceAll('_', ' ')}</h3><p>{reason.detail}</p></div></article>) : <p className="muted">No scoring reasons increased this customer's risk.</p>}</div></section><section className="panel"><div className="panel-title"><div><p className="eyebrow">Lifecycle</p><h2>Linked work</h2></div></div><h3 className="subheading">Alerts</h3>{alerts.length ? alerts.map(item => <div className="linked-row" key={item.id}><span>{item.rule_code.replaceAll('_', ' ')}</span><span>{badge(item.severity)} {badge(item.status)}</span></div>) : <p className="muted">No monitoring alerts.</p>}<h3 className="subheading">Cases</h3>{cases.length ? cases.map(item => <div className="linked-row" key={item.id}><span>{item.title}</span><span>{badge(item.priority)} {badge(item.status)}</span></div>) : <p className="muted">No investigations.</p>}</section></div><section className="panel"><div className="panel-title"><div><p className="eyebrow">Financial activity</p><h2>Transaction history</h2></div><span>{transactions.length} records</span></div>{transactions.length ? <div className="table-wrap"><table><thead><tr><th>Reference</th><th>Direction</th><th>Amount</th><th>Counterparty</th><th>Occurred</th></tr></thead><tbody>{transactions.map(item => <tr key={item.id}><td><strong>{item.external_ref || item.id.slice(0, 8)}</strong></td><td>{item.direction}</td><td>{formatAmount(item)}</td><td>{item.counterparty_country}</td><td>{new Date(item.occurred_at).toLocaleString()}</td></tr>)}</tbody></table></div> : <Empty text="No transactions" detail="No financial activity has been recorded for this customer."/>}</section><section className="panel audit-explorer"><div className="panel-title"><div><p className="eyebrow">Immutable evidence</p><h2>Audit explorer</h2></div><div className="audit-filters">{['all', 'customer', 'transaction', 'alert', 'case'].map(kind => <button className={filter === kind ? 'active' : ''} key={kind} onClick={() => setFilter(kind)}>{kind}</button>)}</div></div>{visibleActivity.length ? <div className="activity-list">{visibleActivity.map(event => <article key={event.id}><span className={`activity-icon ${event.aggregate_type}`}>{event.aggregate_type[0].toUpperCase()}</span><div><strong>{event.event_type.replaceAll('.', ' ')}</strong><p>{event.actor} · {event.aggregate_type} {event.aggregate_id.slice(0, 8)}</p></div><time>{new Date(event.occurred_at).toLocaleString()}</time></article>)}</div> : <Empty text="No matching events" detail="Choose another activity filter."/>}</section></>}</section>
}

function CDDWorkspace({customers, token, role}) {
  const [customerID, setCustomerID] = useState(customers[0]?.id || '')
  const [details, setDetails] = useState(null)
  const [error, setError] = useState('')
  const canEdit = role === 'analyst' || role === 'admin'
  const canReview = role === 'reviewer' || role === 'admin'
  const load = async () => { if (!customerID) return; setError(''); try { setDetails(await api(`/v1/customers/${customerID}/due-diligence`, token)) } catch (e) { setError(e.message) } }
  useEffect(() => { load() }, [customerID, token])
  const updateProfile = async () => {
    const source_of_wealth = prompt('Source of wealth', details.profile.source_of_wealth || ''); if (!source_of_wealth) return
    const business_purpose = prompt('Business purpose', details.profile.business_purpose || ''); if (!business_purpose) return
    const volume = prompt('Expected monthly volume', String((details.profile.expected_monthly_volume_minor || 0) / 100)); if (volume === null) return
    const next = prompt('Next review date (YYYY-MM-DD)', details.profile.next_review_at?.slice(0, 10) || '')
    await api(`/v1/customers/${customerID}/due-diligence`, token, {method:'PUT', body:JSON.stringify({source_of_wealth,business_purpose,expected_monthly_volume_minor:Math.round(Number(volume)*100),currency:details.profile.currency||'GBP',status:'in_review',next_review_at:next?new Date(next).toISOString():null})}); await load()
  }
  const addOwner = async () => { const full_name=prompt('Beneficial owner full name');if(!full_name)return;const ownership=prompt('Ownership percentage');if(!ownership)return;const country_code=prompt('Country code','GB');if(!country_code)return;await api(`/v1/customers/${customerID}/beneficial-owners`,token,{method:'POST',body:JSON.stringify({full_name,ownership_percent:Number(ownership),country_code,pep:confirm('Is this owner a politically exposed person?')})});await load() }
  const addDocument = async () => { const type=prompt('Document type','incorporation_certificate');if(!type)return;const reference=prompt('Document reference');if(!reference)return;await api(`/v1/customers/${customerID}/kyc-documents`,token,{method:'POST',body:JSON.stringify({type,reference})});await load() }
  const review = async (id,status) => { await api(`/v1/kyc-documents/${id}/review`,token,{method:'POST',body:JSON.stringify({status})});await load() }
  if (!customers.length) return <section className="panel"><Empty text="No customers available" detail="Register a customer before starting due diligence."/></section>
  return <section className="cdd-page"><section className="panel"><div className="panel-title"><div><p className="eyebrow">Customer due diligence</p><h2>KYC review workspace</h2></div><label className="customer-picker"><span>Customer</span><select value={customerID} onChange={event=>setCustomerID(event.target.value)}>{customers.map(customer=><option key={customer.id} value={customer.id}>{customer.legal_name}</option>)}</select></label></div></section>{error&&<div className="error">{error}</div>}{!details?<div className="loading">Loading due diligence record…</div>:<><section className="panel cdd-profile"><div className="panel-title"><div><p className="eyebrow">CDD profile</p><h2>Purpose, wealth and periodic review</h2></div><div className="panel-actions">{badge(details.profile.status)}{canEdit&&<button onClick={updateProfile}>Update profile</button>}</div></div><div className="cdd-summary"><div><span>Source of wealth</span><strong>{details.profile.source_of_wealth||'Not recorded'}</strong></div><div><span>Business purpose</span><strong>{details.profile.business_purpose||'Not recorded'}</strong></div><div><span>Expected monthly volume</span><strong>{details.profile.currency?new Intl.NumberFormat(undefined,{style:'currency',currency:details.profile.currency}).format(details.profile.expected_monthly_volume_minor/100):'Not recorded'}</strong></div><div><span>Next review</span><strong>{details.profile.next_review_at?new Date(details.profile.next_review_at).toLocaleDateString():'Not scheduled'}</strong></div></div></section><div className="cdd-columns"><section className="panel"><div className="section-heading"><div><p className="eyebrow">Ownership verification</p><h2>Beneficial owners</h2></div>{canEdit&&<button className="compact" onClick={addOwner}>Add owner</button>}</div>{details.beneficial_owners.length?details.beneficial_owners.map(owner=><article className="owner-card" key={owner.id}><div className="avatar">{owner.full_name[0]}</div><div><strong>{owner.full_name}</strong><span>{owner.country_code} · {owner.ownership_percent}% ownership</span></div>{owner.pep&&badge('high')}</article>):<Empty text="No beneficial owners" detail="Ownership information has not been captured."/>}</section><section className="panel"><div className="section-heading"><div><p className="eyebrow">Evidence register</p><h2>KYC documents</h2></div>{canEdit&&<button className="compact" onClick={addDocument}>Add document</button>}</div>{details.documents.length?details.documents.map(document=><article className="document-card" key={document.id}><div><strong>{document.type.replaceAll('_',' ')}</strong><span>{document.reference} · added by {document.created_by}</span></div><div>{badge(document.status)}{canReview&&document.status==='pending'&&<span className="document-actions"><button onClick={()=>review(document.id,'verified')}>Verify</button><button className="reject" onClick={()=>review(document.id,'rejected')}>Reject</button></span>}</div></article>):<Empty text="No KYC documents" detail="Document evidence has not been registered."/>}</section></div></>}</section>
}

function ScreeningWorkspace({customers, token, role}) {
  const [customerID,setCustomerID]=useState(customers[0]?.id||'');const[matches,setMatches]=useState([]);const[loading,setLoading]=useState(false);const[error,setError]=useState('')
  const canRun=role==='analyst'||role==='admin';const canReview=role==='reviewer'||role==='admin'
  const load=async()=>{if(!customerID)return;setError('');try{const page=await api(`/v1/customers/${customerID}/screening-matches`,token);setMatches(page.items||[])}catch(e){setError(e.message)}}
  useEffect(()=>{load()},[customerID,token])
  const run=async()=>{setLoading(true);setError('');try{await api(`/v1/customers/${customerID}/screenings`,token,{method:'POST'});await load()}catch(e){setError(e.message)}finally{setLoading(false)}}
  const disposition=async(match,status)=>{const reason=prompt(`Reason for ${status.replaceAll('_',' ')}`);if(!reason)return;await api(`/v1/screening-matches/${match.id}/disposition`,token,{method:'POST',body:JSON.stringify({status,reason})});await load()}
  if(!customers.length)return <section className="panel"><Empty text="No customers available" detail="Register a customer before screening."/></section>
  const potential=matches.filter(item=>item.status==='potential').length
  return <section className="screening-page"><section className="screening-hero panel"><div><p className="eyebrow">Financial crime intelligence</p><h2>Sanctions, PEP & adverse media screening</h2><p>Deterministic demo-provider with explainable name similarity scoring.</p></div><div className="screening-controls"><select value={customerID} onChange={event=>setCustomerID(event.target.value)}>{customers.map(customer=><option key={customer.id} value={customer.id}>{customer.legal_name}</option>)}</select>{canRun&&<button disabled={loading} onClick={run}>{loading?'Screening…':'Run screening'}</button>}</div></section>{error&&<div className="error">{error}</div>}<section className="customer-metrics screening-metrics"><article><span>Total matches</span><strong>{matches.length}</strong><small>All screening history</small></article><article><span>Potential</span><strong>{potential}</strong><small>Requires human review</small></article><article><span>Confirmed</span><strong>{matches.filter(item=>item.status==='confirmed').length}</strong><small>True matches</small></article><article><span>False positive</span><strong>{matches.filter(item=>item.status==='false_positive').length}</strong><small>Disposition recorded</small></article></section><section className="panel"><div className="panel-title"><div><p className="eyebrow">Match review queue</p><h2>Screening results</h2></div><span>{potential} awaiting disposition</span></div>{matches.length?<div className="match-list">{matches.map(match=><article key={match.id}><div className="match-score"><strong>{match.score}</strong><span>match</span></div><div><h3>{match.query_name} ↔ {match.matched_name}</h3><p>{match.subject_type.replaceAll('_',' ')} · {match.reason}</p><div>{badge(match.list_type)} {badge(match.status)}</div></div><div className="match-review">{match.reviewed_by?<><strong>{match.reviewed_by}</strong><span>{match.disposition_reason}</span></>:canReview&&match.status==='potential'?<><button onClick={()=>disposition(match,'confirmed')}>Confirm</button><button className="reject" onClick={()=>disposition(match,'false_positive')}>False positive</button></>:<span>Review required</span>}</div></article>)}</div>:<Empty text="No screening matches" detail="Run screening to check the customer and beneficial owners."/>}</section></section>
}

function CustomerForm({token, onCancel, onComplete}) {
  const [form, setForm] = useState({type: 'individual', legal_name: '', external_ref: '', country_code: 'GB', country_risk: 'low', source_of_funds_verified: true, pep: false, sanctions_potential_match: false, high_risk_industry: false, complex_ownership: false})
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const update = event => setForm({...form, [event.target.name]: event.target.type === 'checkbox' ? event.target.checked : event.target.value})
  const submit = async event => {
    event.preventDefault(); setSubmitting(true); setError('')
    const {type, legal_name, external_ref, country_code, country_risk, ...risk} = form
    try {
      await api('/v1/customers', token, {method: 'POST', body: JSON.stringify({type, legal_name: legal_name.trim(), external_ref: external_ref.trim(), country_code: country_code.toUpperCase(), risk_factors: {country_risk, ...risk}})})
      await onComplete()
    } catch (e) { setError(e.message) } finally { setSubmitting(false) }
  }
  return <section className="panel form-panel"><div className="panel-title"><div><p className="eyebrow">Customer onboarding</p><h2>Register and assess an entity</h2></div><button className="secondary" onClick={onCancel}>Cancel</button></div><p className="form-intro">The platform calculates risk using the controlled scoring rules and submits the customer for independent approval.</p>{error && <div className="error">{error}</div>}<form onSubmit={submit}><div className="form-grid"><Field label="Customer type"><select name="type" value={form.type} onChange={update}><option value="individual">Individual</option><option value="company">Company</option></select></Field><Field label="Legal name"><input required maxLength="200" name="legal_name" value={form.legal_name} onChange={update} placeholder="Example Trading Ltd"/></Field><Field label="External reference"><input maxLength="100" name="external_ref" value={form.external_ref} onChange={update} placeholder="CRM-1042"/></Field><Field label="Country code"><input required minLength="2" maxLength="2" name="country_code" value={form.country_code} onChange={update}/></Field><Field label="Country risk"><select name="country_risk" value={form.country_risk} onChange={update}><option value="low">Low</option><option value="medium">Medium</option><option value="high">High</option></select></Field></div><fieldset><legend>Compliance risk factors</legend><div className="checks"><Check name="source_of_funds_verified" checked={form.source_of_funds_verified} onChange={update} label="Source of funds verified"/><Check name="pep" checked={form.pep} onChange={update} label="Politically exposed person"/><Check name="sanctions_potential_match" checked={form.sanctions_potential_match} onChange={update} label="Potential sanctions match"/><Check name="high_risk_industry" checked={form.high_risk_industry} onChange={update} label="High-risk industry"/><Check name="complex_ownership" checked={form.complex_ownership} onChange={update} label="Complex ownership"/></div></fieldset><div className="form-footer"><p>Customer and audit event will be written atomically.</p><button disabled={submitting}>{submitting ? 'Assessing…' : 'Assess risk & submit'}</button></div></form></section>
}

function TransactionForm({token, customers, initialCustomer, onCancel, onComplete}) {
  const [form, setForm] = useState({customer_id: initialCustomer?.id || customers[0]?.id || '', external_ref: '', direction: 'inbound', amount: '', currency: 'GBP', counterparty_country: 'GB', occurred_at: new Date().toISOString().slice(0, 16)})
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState(null)
  const update = event => setForm({...form, [event.target.name]: event.target.value})
  const submit = async event => {
    event.preventDefault(); setSubmitting(true); setError(''); setResult(null)
    try {
      const response = await api('/v1/transactions', token, {method: 'POST', headers: {'Idempotency-Key': crypto.randomUUID()}, body: JSON.stringify({customer_id: form.customer_id, external_ref: form.external_ref.trim(), direction: form.direction, amount_minor: Math.round(Number(form.amount) * 100), currency: form.currency.toUpperCase(), counterparty_country: form.counterparty_country.toUpperCase(), occurred_at: new Date(form.occurred_at).toISOString()})})
      setResult(response); await onComplete()
    } catch (e) { setError(e.message) } finally { setSubmitting(false) }
  }
  if (result) return <section className="panel result-panel"><div className="success-mark">✓</div><p className="eyebrow">Transaction monitored</p><h2>{result.alerts?.length ? `${result.alerts.length} alert${result.alerts.length === 1 ? '' : 's'} generated` : 'No suspicious activity detected'}</h2><p>The transaction and its audit event were persisted successfully.</p><button onClick={onCancel}>Return to customers</button></section>
  return <section className="panel form-panel"><div className="panel-title"><div><p className="eyebrow">Transaction monitoring</p><h2>Ingest a customer transaction</h2></div><button className="secondary" onClick={onCancel}>Cancel</button></div><p className="form-intro">Amounts are entered in major currency units and stored precisely as integer minor units.</p>{error && <div className="error">{error}</div>}<form onSubmit={submit}><div className="form-grid"><Field label="Active customer"><select required name="customer_id" value={form.customer_id} onChange={update}>{customers.map(customer => <option key={customer.id} value={customer.id}>{customer.legal_name}</option>)}</select></Field><Field label="Direction"><select name="direction" value={form.direction} onChange={update}><option value="inbound">Inbound</option><option value="outbound">Outbound</option></select></Field><Field label="Amount"><input required min="0.01" step="0.01" type="number" name="amount" value={form.amount} onChange={update} placeholder="10000.00"/></Field><Field label="Currency"><input required minLength="3" maxLength="3" name="currency" value={form.currency} onChange={update}/></Field><Field label="Counterparty country"><input required minLength="2" maxLength="2" name="counterparty_country" value={form.counterparty_country} onChange={update}/></Field><Field label="Occurred at"><input required type="datetime-local" name="occurred_at" value={form.occurred_at} onChange={update}/></Field><Field label="External reference"><input maxLength="100" name="external_ref" value={form.external_ref} onChange={update} placeholder="PAY-2026-001"/></Field></div><div className="form-footer"><p>Monitoring rules run automatically during ingestion.</p><button disabled={submitting || !customers.length}>{submitting ? 'Monitoring…' : 'Ingest & monitor'}</button></div></form></section>
}

const Field = ({label, children}) => <label className="field"><span>{label}</span>{children}</label>
const Check = ({label, ...props}) => <label className="check"><input type="checkbox" {...props}/><span>{label}</span></label>

function Approvals({items, token, role, reload}) {
  const canReview = role === 'reviewer' || role === 'admin'
  const act = async (id, decision) => { const reason = prompt(`Reason for ${decision}`); if (reason === null) return; await api(`/v1/customers/${id}/${decision}`, token, {method: 'POST', body: JSON.stringify({reason})}); reload() }
  return <section className="panel"><div className="panel-title"><div><p className="eyebrow">Maker-checker</p><h2>Independent review queue</h2></div></div>{items.length === 0 ? <Empty text="No customers awaiting approval"/> : <div className="cards">{items.map(customer => <article className="review-card" key={customer.id}><div><h3>{customer.legal_name}</h3><p>{customer.country_code} · score {customer.risk_assessment.score} · submitted by {customer.created_by}</p></div><div>{badge(customer.risk_assessment.rating)}{canReview && <><button onClick={() => act(customer.id, 'approve')}>Approve</button><button className="danger" onClick={() => act(customer.id, 'reject')}>Reject</button></>}</div></article>)}</div>}</section>
}
function Alerts({items, cases, token, role, reload}) {
  const canClose = role === 'reviewer' || role === 'admin'
  const createCase = async alert => { const title = prompt('Investigation title', `Investigate ${alert.rule_code.replaceAll('_', ' ')}`); if (!title) return; const priority = prompt('Priority: low, medium or high', alert.severity) || alert.severity; await api('/v1/cases', token, {method: 'POST', body: JSON.stringify({alert_id: alert.id, title, priority})}); reload() }
  const close = async id => { const reason = prompt('Document the investigation outcome'); if (!reason) return; await api(`/v1/alerts/${id}/close`, token, {method: 'POST', body: JSON.stringify({reason})}); reload() }
  return <section className="panel"><div className="panel-title"><div><p className="eyebrow">Explainable monitoring</p><h2>Alert investigation queue</h2></div></div><AlertTable items={items} onClose={canClose ? close : null} onCreateCase={createCase} caseAlertIDs={new Set(cases.map(item => item.alert_id))}/></section>
}
function AlertTable({items, onClose, onCreateCase, caseAlertIDs = new Set()}) {
  return items.length === 0 ? <Empty text="No monitoring alerts"/> : <div className="table-wrap"><table><thead><tr><th>Rule</th><th>Reason</th><th>Severity</th><th>Status</th><th>Created</th><th></th></tr></thead><tbody>{items.map(alert => <tr key={alert.id}><td><strong>{alert.rule_code.replaceAll('_', ' ')}</strong><small>{alert.rule_version}</small></td><td>{alert.description}</td><td>{badge(alert.severity)}</td><td>{badge(alert.status)}</td><td>{new Date(alert.created_at).toLocaleString()}</td><td><div className="row-actions">{onCreateCase && alert.status === 'open' && !caseAlertIDs.has(alert.id) && <button className="compact" onClick={() => onCreateCase(alert)}>Open case</button>}{onClose && alert.status === 'open' && !caseAlertIDs.has(alert.id) && <button className="compact" onClick={() => onClose(alert.id)}>Close</button>}</div></td></tr>)}</tbody></table></div>
}

function Cases({items, token, role, reload}) {
  const [selected, setSelected] = useState(null)
  const [details, setDetails] = useState(null)
  const [comment, setComment] = useState('')
  const [error, setError] = useState('')
  const canManage = role === 'reviewer' || role === 'admin'
  const open = async item => { setSelected(item); setError(''); try { setDetails(await api(`/v1/cases/${item.id}`, token)) } catch (e) { setError(e.message) } }
  const refreshDetails = async () => { await reload(); setDetails(await api(`/v1/cases/${selected.id}`, token)) }
  const assign = async () => { const assignee = prompt('Assign investigator', details.case.assigned_to || ''); if (!assignee) return; await api(`/v1/cases/${selected.id}/assign`, token, {method: 'POST', body: JSON.stringify({assignee})}); await refreshDetails() }
  const addComment = async event => { event.preventDefault(); if (!comment.trim()) return; await api(`/v1/cases/${selected.id}/comments`, token, {method: 'POST', body: JSON.stringify({body: comment})}); setComment(''); await refreshDetails() }
  const resolve = async () => { const resolution = prompt('Document the final investigation decision'); if (!resolution) return; await api(`/v1/cases/${selected.id}/resolve`, token, {method: 'POST', body: JSON.stringify({resolution})}); await refreshDetails() }
  if (selected) return <section className="case-workspace"><button className="secondary" onClick={() => { setSelected(null); setDetails(null) }}>← All cases</button>{error && <div className="error">{error}</div>}{!details ? <div className="loading">Loading investigation…</div> : <><div className="case-head panel"><div><p className="eyebrow">Investigation case</p><h2>{details.case.title}</h2><p>Customer {details.case.customer_id.slice(0, 8)} · Alert {details.case.alert_id.slice(0, 8)}</p></div><div>{badge(details.case.priority)} {badge(details.case.status)}</div></div><div className="case-grid"><section className="panel"><div className="panel-title"><h2>Investigation notes</h2></div>{details.comments.length === 0 && <p className="muted">No comments have been added.</p>}<div className="comment-list">{details.comments.map(item => <article key={item.id}><strong>{item.author}</strong><time>{new Date(item.created_at).toLocaleString()}</time><p>{item.body}</p></article>)}</div>{details.case.status !== 'resolved' && <form className="comment-form" onSubmit={addComment}><textarea required value={comment} onChange={event => setComment(event.target.value)} placeholder="Record evidence, analysis or next steps"/><button>Add note</button></form>}</section><aside className="case-side panel"><h3>Case controls</h3><dl><dt>Status</dt><dd>{details.case.status.replaceAll('_', ' ')}</dd><dt>Assigned to</dt><dd>{details.case.assigned_to || 'Unassigned'}</dd><dt>Created by</dt><dd>{details.case.created_by}</dd><dt>Updated</dt><dd>{new Date(details.case.updated_at).toLocaleString()}</dd></dl>{canManage && details.case.status !== 'resolved' && <><button onClick={assign}>Assign investigator</button><button className="resolve" onClick={resolve}>Resolve case & close alert</button></>}{details.case.resolution && <div className="resolution"><strong>Resolution</strong><p>{details.case.resolution}</p></div>}</aside></div><section className="panel timeline"><div className="panel-title"><h2>Audit timeline</h2></div>{details.timeline.map(event => <article key={event.id}><span></span><div><strong>{event.event_type.replaceAll('.', ' ')}</strong><p>{event.actor} · {new Date(event.occurred_at).toLocaleString()}</p></div></article>)}</section></>}</section>
  return <section className="panel"><div className="panel-title"><div><p className="eyebrow">Case management</p><h2>Investigation workspace</h2></div><span>{items.filter(item => item.status !== 'resolved').length} active</span></div>{items.length === 0 ? <Empty text="No investigation cases" detail="Open a case from a monitoring alert to begin an investigation."/> : <div className="case-list">{items.map(item => <button key={item.id} onClick={() => open(item)}><div><strong>{item.title}</strong><span>Customer {item.customer_id.slice(0, 8)} · {item.assigned_to || 'Unassigned'}</span></div><div>{badge(item.priority)} {badge(item.status)}<small>{new Date(item.updated_at).toLocaleDateString()}</small></div></button>)}</div>}</section>
}
const Empty = ({text, detail = 'The operational queue is clear.'}) => <div className="empty"><span>✓</span><h3>{text}</h3><p>{detail}</p></div>
createRoot(document.getElementById('root')).render(<App/>)
