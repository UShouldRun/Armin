"""A small science reference library for web_search, so science
tasks have something to look up: the CODATA constants (NIST,
public domain), the periodic table (PubChem, public domain) and a
hand-written formula sheet. Its pids start with "ref-"; search
tasks never see it (env.web_search), so their corpus is unchanged.
Standard library only."""

import csv
import io

import fetch
from common import read_jsonl, write_jsonl

CODATA   = "https://physics.nist.gov/cuu/Constants/Table/allascii.txt"
PUBCHEM  = "https://pubchem.ncbi.nlm.nih.gov/rest/pug/periodictable/CSV"
NIST_URL = "https://physics.nist.gov/cuu/Constants/"
PREFIX   = "ref-"

# (title, text). Values are the usual textbook ones; the constants
# table has the exact CODATA figures.
FORMULAS = [
  ("Kinematics, constant acceleration",
   "v = v0 + a t; x = x0 + v0 t + (1/2) a t^2; v^2 = v0^2 + 2 a (x - x0); "
   "average velocity (v0 + v)/2. Free fall near Earth: a = g = 9.81 m/s^2 "
   "downward (9.80665 m/s^2 standard gravity)."),
  ("Projectile motion",
   "Horizontal: x = v0 cos(theta) t. Vertical: y = v0 sin(theta) t - (1/2) g t^2. "
   "Range on level ground R = v0^2 sin(2 theta) / g; maximum height "
   "H = (v0 sin theta)^2 / (2 g); time of flight T = 2 v0 sin(theta) / g."),
  ("Newton's laws and friction",
   "F_net = m a. Weight W = m g. Kinetic friction f = mu_k N, static "
   "friction f <= mu_s N. On an incline of angle theta: N = m g cos(theta), "
   "component along the slope m g sin(theta)."),
  ("Work, energy and power",
   "W = F d cos(theta). Kinetic energy K = (1/2) m v^2. Gravitational "
   "potential energy U = m g h. Spring: F = -k x, U = (1/2) k x^2. "
   "Power P = W / t = F v. Work-energy theorem: W_net = delta K."),
  ("Momentum and collisions",
   "p = m v; impulse J = F delta t = delta p. Momentum is conserved in "
   "collisions. Perfectly inelastic: v_f = (m1 v1 + m2 v2)/(m1 + m2). "
   "1D elastic: v1' = ((m1 - m2) v1 + 2 m2 v2)/(m1 + m2)."),
  ("Circular motion and rotation",
   "Centripetal acceleration a_c = v^2 / r = omega^2 r; v = omega r; "
   "period T = 2 pi r / v. Torque tau = r F sin(theta) = I alpha. "
   "Rotational KE = (1/2) I omega^2; angular momentum L = I omega. "
   "Moments of inertia: solid sphere (2/5) m r^2, hollow sphere (2/3) m r^2, "
   "solid cylinder (1/2) m r^2, hoop m r^2, rod about center (1/12) m L^2, "
   "rod about end (1/3) m L^2."),
  ("Gravitation and orbits",
   "F = G m1 m2 / r^2 (G = 6.674e-11 N m^2/kg^2). Surface gravity "
   "g = G M / R^2. Circular orbit speed v = sqrt(G M / r); escape speed "
   "sqrt(2 G M / r). Kepler's third law T^2 = 4 pi^2 r^3 / (G M). "
   "Earth: mass 5.972e24 kg, radius 6.371e6 m. Sun: mass 1.989e30 kg."),
  ("Oscillations and waves",
   "Mass on spring: omega = sqrt(k/m), T = 2 pi sqrt(m/k). Simple pendulum "
   "T = 2 pi sqrt(L/g). Wave speed v = f lambda; on a string v = sqrt(T/mu). "
   "Speed of sound in air about 343 m/s at 20 C. Doppler: "
   "f' = f (v + v_observer)/(v - v_source), signs positive when approaching. "
   "Sound level beta = 10 log10(I / 1e-12 W/m^2) dB."),
  ("Fluids",
   "Density rho = m/V; pressure P = F/A; hydrostatic P = P0 + rho g h. "
   "Buoyant force F_b = rho_fluid g V_displaced. Continuity A1 v1 = A2 v2. "
   "Bernoulli: P + (1/2) rho v^2 + rho g h = constant. Water density "
   "1000 kg/m^3; atmospheric pressure 101325 Pa. Reynolds number "
   "Re = rho v D / mu. Poiseuille flow Q = pi r^4 delta P / (8 mu L)."),
  ("Thermodynamics and heat",
   "Heat Q = m c delta T; latent heat Q = m L. Water: c = 4186 J/(kg K) "
   "(4.184 J/(g C)), latent heat of fusion 3.34e5 J/kg, of vaporization "
   "2.26e6 J/kg. First law delta U = Q - W. Isothermal ideal-gas work "
   "W = n R T ln(V2/V1). Carnot efficiency 1 - T_c/T_h. Entropy "
   "delta S = Q/T. Linear expansion delta L = alpha L delta T. "
   "Conduction P = k A delta T / d. Stefan-Boltzmann P = e sigma A T^4."),
  ("Ideal gas and kinetic theory",
   "P V = n R T, R = 8.314 J/(mol K) = 0.08206 L atm/(mol K). "
   "P V = N k_B T. Average translational KE (3/2) k_B T; rms speed "
   "sqrt(3 R T / M). Molar volume at STP (0 C, 1 atm) 22.414 L/mol. "
   "Dalton: P_total = sum of partial pressures. Combined gas law "
   "P1 V1 / T1 = P2 V2 / T2 (temperatures in kelvin, K = C + 273.15)."),
  ("Electrostatics",
   "Coulomb's law F = k q1 q2 / r^2, k = 8.988e9 N m^2/C^2 = 1/(4 pi eps0). "
   "Field E = F/q = k q / r^2; potential V = k q / r; energy U = q V. "
   "Capacitance C = Q/V; parallel plate C = eps0 A / d; energy "
   "(1/2) C V^2. Capacitors in parallel add; in series 1/C = sum 1/C_i."),
  ("Circuits",
   "Ohm's law V = I R. Power P = I V = I^2 R = V^2 / R. Resistors in "
   "series add; in parallel 1/R = sum 1/R_i. RC time constant tau = R C; "
   "charging V(t) = V0 (1 - e^(-t/RC)). Resistance R = rho L / A. "
   "Kirchhoff: currents into a node sum to zero; voltages around a loop "
   "sum to zero. Energy E = P t (1 kWh = 3.6e6 J)."),
  ("Magnetism and induction",
   "Force on a charge F = q v B sin(theta); on a wire F = I L B sin(theta). "
   "Cyclotron radius r = m v / (q B). Field of a long wire B = mu0 I / (2 pi r); "
   "solenoid B = mu0 n I (mu0 = 4 pi e-7 T m/A). Faraday's law "
   "emf = -N d(Phi)/dt, Phi = B A cos(theta). Transformer "
   "V_s / V_p = N_s / N_p. Inductor energy (1/2) L I^2."),
  ("Optics",
   "Speed of light c = 2.998e8 m/s; n = c / v. Snell's law "
   "n1 sin(theta1) = n2 sin(theta2). Thin lens and mirror "
   "1/f = 1/d_o + 1/d_i; magnification m = -d_i / d_o. Mirror f = R/2. "
   "Double slit bright fringes d sin(theta) = m lambda; single slit "
   "dark fringes a sin(theta) = m lambda. Critical angle sin(theta_c) = n2/n1."),
  ("Modern physics",
   "Photon energy E = h f = h c / lambda (h = 6.626e-34 J s, "
   "h c = 1240 eV nm). Photoelectric effect K_max = h f - phi. "
   "de Broglie lambda = h / p. Mass-energy E = m c^2; 1 u = 931.494 MeV/c^2. "
   "Lorentz factor gamma = 1/sqrt(1 - v^2/c^2); time dilation t = gamma t0; "
   "length contraction L = L0 / gamma. Hydrogen levels E_n = -13.6 eV / n^2."),
  ("Radioactive decay and kinetics",
   "N(t) = N0 e^(-lambda t) = N0 (1/2)^(t / t_half); lambda = ln 2 / t_half. "
   "Activity A = lambda N. First-order reaction: ln[A] = ln[A]0 - k t, "
   "t_half = 0.693 / k. Second order: 1/[A] = 1/[A]0 + k t. Zero order: "
   "[A] = [A]0 - k t. Arrhenius k = A e^(-Ea / (R T)); "
   "ln(k2/k1) = (Ea/R)(1/T1 - 1/T2)."),
  ("Moles, concentration and stoichiometry",
   "n = m / M (molar mass from the periodic table). Avogadro's number "
   "6.022e23 per mol. Molarity M = mol solute / L solution; molality "
   "m = mol solute / kg solvent. Dilution M1 V1 = M2 V2. Percent yield = "
   "actual / theoretical * 100. Mass percent = mass part / mass total * 100. "
   "Limiting reagent: the one giving the least product."),
  ("Acids, bases and buffers",
   "pH = -log10[H+]; pOH = -log10[OH-]; pH + pOH = 14 at 25 C "
   "(Kw = 1.0e-14). Ka Kb = Kw; pKa = -log10 Ka. Weak acid "
   "[H+] approx sqrt(Ka C). Henderson-Hasselbalch pH = pKa + log10([A-]/[HA]). "
   "Titration at equivalence: mol acid = mol base."),
  ("Equilibrium and thermochemistry",
   "Kc from concentrations, Kp = Kc (R T)^(delta n). Gibbs energy "
   "delta G = delta H - T delta S; delta G0 = -R T ln K. Hess's law: "
   "delta H adds over steps. delta H_rxn = sum delta H_f(products) - "
   "sum delta H_f(reactants). Calorimetry q = m c delta T, "
   "q_rxn = -q_solution."),
  ("Electrochemistry and solutions",
   "E_cell = E_cathode - E_anode. delta G = -n F E (F = 96485 C/mol). "
   "Nernst: E = E0 - (R T / n F) ln Q = E0 - (0.0592 / n) log10 Q at 25 C. "
   "Charge Q = I t; moles of electrons = Q / F. Colligative: "
   "delta T_f = i K_f m (water K_f = 1.86 C kg/mol), delta T_b = i K_b m "
   "(water K_b = 0.512), osmotic pressure Pi = i M R T. Raoult "
   "P = x_solvent P0. Beer-Lambert A = epsilon l c."),
  ("Population genetics and growth",
   "Hardy-Weinberg: p + q = 1, p^2 + 2 p q + q^2 = 1. Exponential growth "
   "N(t) = N0 e^(r t), doubling time ln 2 / r. Logistic growth "
   "dN/dt = r N (1 - N/K). Monohybrid cross Aa x Aa gives 1:2:1 genotypes, "
   "3:1 phenotypes; dihybrid 9:3:3:1. Probability of independent events "
   "multiplies."),
  ("Microeconomics",
   "Price elasticity of demand = % change in quantity / % change in price; "
   "midpoint method uses averages as bases. Total revenue P Q. Profit = "
   "TR - TC; marginal cost = d(TC)/dQ; profit is maximized where MR = MC. "
   "Average total cost ATC = TC / Q. Consumer surplus is the area under "
   "demand above price; for linear curves (1/2) base height."),
  ("Macroeconomics",
   "GDP = C + I + G + (X - M). GDP deflator = nominal / real * 100. "
   "Inflation rate = (P2 - P1) / P1 * 100. Spending multiplier "
   "1 / (1 - MPC) = 1 / MPS; tax multiplier -MPC / (1 - MPC). Money "
   "multiplier 1 / reserve ratio. Real interest rate approx nominal - "
   "inflation. Unemployment rate = unemployed / labor force * 100."),
  ("Time value of money",
   "Compound interest FV = PV (1 + r/n)^(n t); continuous FV = PV e^(r t). "
   "Present value PV = FV / (1 + r)^t. Annuity PV = PMT (1 - (1 + r)^-n) / r; "
   "FV = PMT ((1 + r)^n - 1) / r. NPV = sum CF_t / (1 + r)^t - initial cost. "
   "Rule of 72: doubling time approx 72 / rate%."),
  ("Mechanics of materials",
   "Stress sigma = F / A; strain epsilon = delta L / L; Young's modulus "
   "E = sigma / epsilon, so delta L = F L / (A E). Shear stress tau = V / A. "
   "Torsion tau = T r / J, J = pi d^4 / 32 for a solid shaft. Beam bending "
   "sigma = M y / I; rectangle I = b h^3 / 12. Steel E approx 200 GPa, "
   "aluminum 69 GPa."),
  ("Unit conversions",
   "1 inch = 2.54 cm; 1 ft = 0.3048 m; 1 mile = 1609.34 m; 1 lb = 0.453592 kg; "
   "1 atm = 101325 Pa = 760 mmHg = 1.01325 bar; 1 cal = 4.184 J; "
   "1 eV = 1.602e-19 J; 1 L = 1e-3 m^3; 1 hp = 745.7 W; 1 km/h = 1/3.6 m/s; "
   "T(F) = 1.8 T(C) + 32; 1 year = 3.156e7 s."),
]


def constants():
  with open(fetch.file(CODATA, "codata.txt"), encoding = "utf-8", errors = "replace") as f:
    text = f.read()

  lines = text.splitlines()
  start = next(i for i, l in enumerate(lines) if l.startswith("-----")) + 1
  docs = []
  for l in lines[start:]:
    if not l.strip():
      continue

    name = l[:60].strip()
    value = l[60:85].replace(" ", "").replace("...", "")
    unc = l[85:110].replace(" ", "")
    unit = l[110:].strip()
    exact = "exact" in unc
    docs.append({
      "title": name,
      "text":  (
        f"CODATA 2022 value of the {name}: {value}"
        + (f" {unit}" if unit else "")
        + (" (exact)." if exact else f", standard uncertainty {unc}.")
      ),
      "url":   NIST_URL,
    })

  return docs


def elements():
  with open(fetch.file(PUBCHEM, "pubchem_elements.csv"), encoding = "utf-8", errors = "replace") as f:
    text = f.read()

  docs = []
  for r in csv.DictReader(io.StringIO(text)):
    parts = [
      f"{r['Name']} ({r['Symbol']}): atomic number {r['AtomicNumber']}, "
      f"atomic mass {r['AtomicMass']} u (molar mass {r['AtomicMass']} g/mol)"
    ]
    for key, label, unit in (
      ("ElectronConfiguration", "electron configuration", ""),
      ("Electronegativity",     "electronegativity (Pauling)", ""),
      ("IonizationEnergy",      "first ionization energy", " eV"),
      ("OxidationStates",       "oxidation states", ""),
      ("StandardState",         "standard state", ""),
      ("MeltingPoint",          "melting point", " K"),
      ("BoilingPoint",          "boiling point", " K"),
      ("Density",               "density", " g/cm^3"),
      ("GroupBlock",            "group", ""),
    ):
      if r.get(key):
        parts.append(f"{label} {r[key]}{unit}")

    docs.append({
      "title": f"{r['Name']} (element)",
      "text":  ", ".join(parts) + ".",
      "url":   "https://pubchem.ncbi.nlm.nih.gov/element/" + r["Name"],
    })

  return docs


def docs():
  out = [
    { "title": t, "text": x, "url": "https://en.wikipedia.org/wiki/" + t.replace(" ", "_") }
    for t, x in FORMULAS
  ]
  out += constants() + elements()
  for i, d in enumerate(out):
    d["pid"] = f"{PREFIX}{i}"

  return out


def append(corpus_path):
  """Adds the library to the corpus file, replacing an older copy."""
  rows = [d for d in read_jsonl(corpus_path) if not d["pid"].startswith(PREFIX)]
  refs = docs()
  write_jsonl(corpus_path, rows + refs)
  print(f"science reference: {len(refs)} entries")
