# MVP analizy rentowności sprzedaży Allegro

Stan specyfikacji: 2026-07-16. Dokument opisuje zakres produktu, a nie jego
implementację. Podstawą integracji jest aktualna dokumentacja
[Allegro REST API](https://developer.allegro.pl/documentation/) oraz poradniki
Allegro dotyczące [zamówień](https://developer.allegro.pl/tutorials/jak-obslugiwac-zamowienia-GRaj0qyvwtR),
[opłat](https://developer.allegro.pl/tutorials/jak-sprawdzic-oplaty-nn9DOL5PASX)
i [autoryzacji](https://developer.allegro.pl/tutorials/uwierzytelnianie-i-autoryzacja-zlq9e75GdIR).

## Problem i cel

Panel Allegro pokazuje sprzedaż, płatności i opłaty, ale nie łączy ich z
wewnętrznym kosztem zakupu produktu ani rzeczywistym kosztem wysyłki. Sprzedawca
widzi więc obrót, ale nie wie od razu, które zamówienie lub oferta zarabia po
uwzględnieniu wszystkich podstawowych kosztów.

MVP ma dać odpowiedź na dwa pytania:

1. Ile faktycznie zarobiliśmy na konkretnym, opłaconym zamówieniu?
2. Które oferty są rentowne w wybranym okresie po zsumowaniu ich zamówień?

Wynik ma być rachunkiem operacyjnym brutto w walucie transakcji, przed podatkiem
dochodowym, kosztami stałymi firmy i kosztem pracy. Kwoty są prezentowane jako
brutto. Rozliczenie VAT i pełna księgowość są poza MVP.

## Główna persona

**Mały lub średni sprzedawca Allegro** prowadzący od kilkudziesięciu do kilku
tysięcy aktywnych ofert. Zna koszt zakupu towaru, ale zwykle przechowuje go w
arkuszu, systemie magazynowym albo ERP. Wysyła samodzielnie lub przez umowę z
przewoźnikiem i nie ma zespołu analitycznego.

Potrzebuje prostego wdrożenia, wyniku możliwego do sprawdzenia na poziomie
zamówienia oraz jasnego oznaczenia brakujących kosztów. Nie powinien być zmuszony
do rozumienia typów wpisów billingowych Allegro ani ręcznego łączenia eksportów.

## Najważniejszy przepływ użytkownika

1. Użytkownik wybiera „Połącz konto Allegro” i udziela aplikacji trzech
   uprawnień tylko do odczytu: ofert, zamówień i opłat.
2. Aplikacja synchronizuje aktywne i zakończone oferty, opłacone zamówienia z
   wybranego okresu oraz powiązane wpisy billingowe.
3. Aplikacja łączy `checkoutForm.id`, `lineItem.offer.id` i identyfikatory z
   wpisów billingowych. Zamówienia anulowane nie wchodzą do wyniku.
4. Użytkownik uzupełnia koszt zakupu produktu według identyfikatora oferty lub
   SKU (`external.id`) oraz wybiera sposób podania kosztu wysyłki: reguła w
   konfiguracji albo import CSV.
5. System pokazuje kontrolę kompletności. Pozycje bez kosztu zakupu lub kosztu
   wysyłki mają status „niepełne” i nie są po cichu traktowane jako koszt zero.
6. Użytkownik otwiera listę zamówień z przychodem, kosztami, zyskiem i marżą,
   następnie przechodzi do szczegółów pojedynczego zamówienia.
7. Widok ofert agreguje te same wyniki dla sprzedanych pozycji w wybranym okresie
   i pokazuje liczbę zamówień oraz udział rekordów z kompletnymi kosztami.

Synchronizacja powinna być przyrostowa po `updatedAt` lub przez dziennik zdarzeń,
ale MVP musi również umożliwiać ponowne przeliczenie okresu po zmianie kosztu.

## Zakres MVP

### W zakresie

- połączenie jednego konta Allegro przez OAuth Authorization Code flow;
- synchronizacja ofert, zamówień i rzeczywistych wpisów billingowych;
- ręczna edycja i import kosztów zakupu oraz kosztów wysyłki;
- rentowność na poziomie zamówienia i oferty dla wybranego okresu;
- jawny status kompletności danych i możliwość prześledzenia składników wyniku;
- obsługa PLN w pierwszej wersji; rekordy w innych walutach pozostają widoczne,
  ale bez łączenia w jedną sumę bez zdefiniowanego kursu.

### Poza zakresem

- implementacja funkcji opisanych w tym dokumencie;
- płatności i subskrypcje produktu;
- kanały sprzedaży inne niż Allegro;
- pełna księgowość, rozliczanie VAT, podatek dochodowy i koszty stałe;
- prognozowana rentowność niesprzedanych ofert;
- automatyczne pobieranie kosztów z ERP, faktur lub API przewoźników;
- pełna obsługa zwrotów częściowych i reklamacji. W MVP anulowane zamówienia są
  wykluczane, a zwroty wymagają korekty/importu i są oznaczone do weryfikacji.

## Model danych i źródła

Źródło `Allegro API` oznacza wartość pobieraną automatycznie. `Konfiguracja`
oznacza wartość lub regułę wpisaną przez użytkownika. `Import` oznacza plik CSV;
import ma pierwszeństwo przed ogólną regułą konfiguracji.

### Konto, oferta i zamówienie

| Pole logiczne | Pole/klucz wejściowy | Źródło | Użycie w MVP |
| --- | --- | --- | --- |
| Konto sprzedawcy | token użytkownika, opcjonalnie `GET /me` | Allegro API | Właściciel synchronizowanych danych; token jest sekretem i nie trafia do logów. |
| ID oferty | `offer.id` / `lineItems[].offer.id` | Allegro API | Klucz łączenia oferty, sprzedaży i opłat. |
| SKU sprzedawcy | `external.id` w danych oferty/pozycji, gdy ustawione | Allegro API | Preferowany klucz importu kosztu; brak SKU nie blokuje użycia ID oferty. |
| Nazwa oferty | `name` | Allegro API | Etykieta w raportach. |
| Cena bieżąca oferty | `sellingMode.price` | Allegro API | Kontekst oferty, nie źródło przychodu historycznego. |
| ID zamówienia | `checkoutForm.id` | Allegro API | Główny klucz wyniku zamówienia i łączenia wpisów billingowych. |
| Data sprzedaży | `lineItems[].boughtAt` | Allegro API | Przypisanie wyniku do okresu. |
| Status zamówienia | `status`, `fulfillment.status` | Allegro API | Wykluczenie anulowanych i oznaczenie zwrotów. |
| Liczba sztuk | `lineItems[].quantity` | Allegro API | Przychód pozycji i koszt zakupu. |
| Waluta | pola pieniężne, np. `lineItems[].price.currency` | Allegro API | Zakaz sumowania różnych walut bez konwersji. |

### Przychody, rabaty i dopłaty

| Pole logiczne | Pole/klucz wejściowy | Źródło | Reguła |
| --- | --- | --- | --- |
| Cena transakcyjna sztuki | `lineItems[].price.amount` | Allegro API | Cena historyczna pozycji; nie zastępuje jej bieżąca cena oferty. |
| Przychód z produktów | `price.amount * quantity` dla każdej pozycji | Allegro API / wyliczenie | Cena transakcyjna zawiera efekt rabatu; rabatu nie odejmuje się drugi raz. |
| Typ rabatu | `lineItems[].discounts[]` | Allegro API | Informacja audytowa (np. kupon, zestaw, Allegro Ceny). Nie używać usuniętego pola rabatów na poziomie całego zamówienia. |
| Dostawa zapłacona przez kupującego | `delivery.cost.amount` | Allegro API | Część przychodu zamówienia, odrębna od kosztu przewoźnika. |
| Dopłaty kupującego | `surcharges[].paidAmount.amount` | Allegro API | Dodatkowy przychód po zamówieniu; każdą dopłatę liczyć raz po jej finalizacji. |
| Wyrównanie Allegro | `lineItems[].reconciliation.value`, `payment.reconciliation` lub dodatni wpis billingowy | Allegro API | Rekompensata rabatu finansowana przez Allegro. Znormalizować po identyfikatorze i mechanizmie (`WALLET`/`BILLING`), aby jej nie zdublować. |
| Korekta/rabat poza API | `order_id`, opcjonalnie `line_item_id`, kwota, waluta, powód | Import | Ręczna korekta tylko gdy nie istnieje już w cenie transakcyjnej lub billing entries. |

Pole `payment.paidAmount` i suma `summary.totalToPay` służą do kontroli uzgodnienia,
nie jako dodatkowe składniki przychodu. Zapobiega to podwójnemu policzeniu ceny
pozycji, dostawy i dopłat.

### Koszty

| Pole logiczne | Pole/klucz wejściowy | Źródło | Reguła |
| --- | --- | --- | --- |
| Prowizja od sprzedaży | ujemne `billingEntries[].value` typu prowizja, powiązane z `order.id`/`offer.id` | Allegro API | Rzeczywista naliczona prowizja, nie estymata z kalkulatora ofert. Korekty prowizji zachowują znak. |
| Inne opłaty Allegro | pozostałe ujemne wpisy `billingEntries[].value` | Allegro API | W MVP widoczne oddzielnie; do kosztu zamówienia wchodzą tylko wpisy powiązane z zamówieniem. Opłaty wyłącznie ofertowe agreguje widok oferty. |
| Typ opłaty | `billingEntries[].type.id/name`; katalog z `GET /billing/billing-types` | Allegro API | Klasyfikacja prowizji, opłaty i korekty bez kodów zaszytych w UI. |
| Koszt zakupu sztuki | `offer_id` lub `external_id`, kwota, waluta, opcjonalnie `valid_from` | Konfiguracja lub import | Wartość użytkownika obowiązująca w chwili sprzedaży. W MVP bez kosztu nie ma kompletnego zysku. |
| Koszt zakupu pozycji | koszt zakupu sztuki razy `quantity` | Wyliczenie | COGS danej pozycji. |
| Rzeczywisty koszt dostawy sprzedawcy | `order_id`, kwota, waluta albo reguła dla metody dostawy | Import lub konfiguracja | Allegro API nie podaje faktury/kosztu z umowy sprzedawcy z przewoźnikiem. Nie utożsamiać z `delivery.cost`. |
| Korekta kosztu | `order_id`/`offer_id`, kwota, waluta, powód | Import | Np. ręczna korekta zwrotu; jawna i audytowalna. |

### Minimalny format importu

Import kosztów produktów: `external_id` albo `offer_id`, `unit_purchase_cost`,
`currency`, opcjonalnie `valid_from`. Import kosztów wysyłki: `order_id`,
`seller_shipping_cost`, `currency`. Import odrzuca wiersze bez klucza, kwoty lub
waluty i raportuje nieznane oferty/zamówienia. Ponowny import tego samego klucza
aktualizuje rekord zamiast go dublować.

## Formuły

Wszystkie działania wykonuje się w jednej walucie i na liczbach dziesiętnych,
bez `float`. Wartości billingowe zachowują znak źródłowy.

Dla zamówienia `o`:

```text
przychód_z_produktów(o) = Σ(cena_transakcyjna_sztuki × liczba_sztuk)

przychód(o) = przychód_z_produktów
            + dostawa_zapłacona_przez_kupującego
            + sfinalizowane_dopłaty
            + wyrównania_finansowane_przez_Allegro
            + ręczne_korekty_przychodu

koszt_Allegro(o) = -(Σ ujemnych wpisów billingowych przypisanych do zamówienia)
                    - (Σ dodatnich korekt tych opłat)

koszt_zakupu(o) = Σ(koszt_zakupu_sztuki_w_dacie_sprzedaży × liczba_sztuk)

koszt(o) = koszt_Allegro
         + rzeczywisty_koszt_dostawy_sprzedawcy
         + koszt_zakupu
         + ręczne_korekty_kosztu

zysk(o) = przychód(o) - koszt(o)

marża(o) = zysk(o) / przychód(o) × 100%
```

Jeśli przychód wynosi zero, marża procentowa jest `N/D`, a zysk kwotowy nadal
jest pokazywany. Rabat finansowany przez sprzedawcę jest już widoczny w niższej
cenie transakcyjnej i nie jest osobnym kosztem. Dodatni wpis billingowy lub
`reconciliation` finansowane przez Allegro zwiększa przychód tylko raz.

### Agregacja do oferty

Przychód i koszty produktów są przypisywane bezpośrednio do pozycji oferty.
Koszt dostawy i wpis billingowy dotyczący całego zamówienia trzeba alokować na
pozycje proporcjonalnie do ich przychodu z produktów. Gdy ten przychód wynosi
zero, alokacja jest proporcjonalna do liczby sztuk. Zaokrąglenia trafiają do
ostatniej pozycji, aby suma pozycji była identyczna z zamówieniem.

```text
zysk_oferty = Σ(przychód_alokowany - koszt_alokowany)
marża_oferty = zysk_oferty / Σ(przychód_alokowany) × 100%
```

Widok oferty zawsze podaje zakres dat i procent zamówień z kompletnymi kosztami.

## Allegro REST API i OAuth

MVP korzysta wyłącznie z tokena użytkownika (`bearer-token-for-user`) i prosi o
minimalne zakresy odczytu. Dokładny scope przypisany do zasobu należy ponownie
sprawdzić w specyfikacji OpenAPI przed implementacją, ponieważ API ewoluuje.

| Endpoint | Cel | Wymagany scope OAuth | Uwagi |
| --- | --- | --- | --- |
| `GET /sale/offers` | Lista ofert sprzedawcy, ID, nazwa, `external.id`, bieżąca cena i status | `allegro:api:sale:offers:read` | Paginacja; bieżąca cena nie zastępuje ceny historycznej zamówienia. |
| `GET /sale/product-offers/{offerId}` | Szczegóły pojedynczej oferty, gdy lista nie zwraca potrzebnego pola | `allegro:api:sale:offers:read` | Pobierać tylko dla ofert wymagających uzupełnienia. |
| `GET /order/checkout-forms` | Lista i przyrostowa synchronizacja zamówień | `allegro:api:orders:read` | Filtrowanie po statusie/czasie, paginacja. |
| `GET /order/checkout-forms/{id}` | Pełne pozycje, cena, dostawa, dopłaty, rabaty i reconciliation | `allegro:api:orders:read` | Źródło szczegółu i ponownego uzgodnienia zamówienia. |
| `GET /order/events` | Przyrostowe wykrywanie zakupu, płatności, dopłaty i anulowania | `allegro:api:orders:read` | Zapisać ostatni poprawnie przetworzony event; okresowo uzgadniać pełnym odczytem. |
| `GET /billing/billing-entries` | Rzeczywiste prowizje, opłaty, zwroty i dodatnie wyrównania | `allegro:api:billing:read` | Filtrowanie po czasie, `order.id`, `offer.id` i typie; wartości są podpisane. |
| `GET /billing/billing-types` | Słownik typów operacji billingowych | `allegro:api:billing:read` | Cache'ować i odświeżać; nie opierać logiki wyłącznie na nazwie wyświetlanej. |

`POST /pricing/offer-fee-preview` jest przydatny w przyszłości do prognozy opłat,
ale nie należy do MVP rentowności historycznej: zwraca kalkulację, nie faktycznie
naliczony koszt. Scope `allegro:api:payments:read` również nie jest minimalnie
potrzebny, ponieważ składniki zamówienia i dopłaty są dostępne w zasobach orders.

## Dane niedostępne lub niewystarczające w Allegro API

Użytkownik musi dostarczyć:

- koszt zakupu produktu i jego zmiany w czasie;
- rzeczywisty koszt przewoźnika, materiałów lub fulfillmentu ponoszony przez
  sprzedawcę;
- korekty poza Allegro, np. rabat udzielony innym kanałem lub koszt zwrotu;
- opcjonalne mapowanie SKU, jeśli `external.id` nie jest ustawione w ofertach.

API nie zapewnia pełnego kosztu własnego sprzedaży ani faktur przewoźników.
Kwota `delivery.cost` to należność od kupującego, nie koszt sprzedawcy. API może
też nie zapewnić jednoznacznego przypisania opłaty ofertowej lub korekty do jednej
pozycji zamówienia; wtedy stosowana jest opisana reguła alokacji, a wynik jest
oznaczony jako „alokowany”.

## Reguły kompletności i jakości

- „Kompletny” wynik wymaga ceny i liczby sztuk z API, wpisów billingowych za
  zsynchronizowany okres, kosztu zakupu każdej pozycji oraz kosztu wysyłki.
- Brak kosztu jest `null`, nigdy automatycznym zerem. Jawna wartość zero wymaga
  potwierdzenia użytkownika.
- Wszystkie komponenty przechowują źródło, czas synchronizacji/importu i wersję
  reguły kosztowej, aby wynik dało się odtworzyć.
- Duplikaty API są eliminowane po identyfikatorach wpisów, dopłat i zamówień.
- Po zmianie kosztu historycznego system przelicza objęty nim okres.
- Pozycje anulowane, zwrócone lub z niezgodnym uzgodnieniem płatności mają
  widoczne ostrzeżenie i nie zasilają „pewnego zysku” bez korekty.

## Minimalne ekrany i kryterium wartości

1. **Połączenie i synchronizacja** — stan OAuth, zakres dat, postęp i błędy.
2. **Uzupełnianie kosztów** — lista braków, edycja oraz import CSV z raportem.
3. **Zamówienia** — data, ID, przychód, prowizja, dostawa, zakup, zysk, marża i
   kompletność; szczegół pokazuje każdy składnik i źródło.
4. **Oferty** — nazwa/SKU, liczba sprzedanych sztuk, przychód, zysk, marża,
   udział kompletnych danych i przejście do zamówień.

MVP spełnia cel, jeśli sprzedawca po połączeniu konta i jednym imporcie kosztów
potrafi wskazać zysk wybranego zamówienia oraz porównać rentowność ofert, a suma
składników szczegółu dokładnie uzgadnia się z pokazanym wynikiem.

## Proponowany podział kolejnych prac technicznych

1. **Model domenowy i kontrakty danych** — encje konta, oferty, zamówienia,
   pozycji, wpisu billingowego, kosztu i wyniku; typ Money; migracje i zasady
   idempotencji.
2. **OAuth Allegro** — Authorization Code flow, bezpieczne przechowywanie i
   odświeżanie tokenów, minimalne scope’y, odłączanie konta.
3. **Klient i synchronizacja ofert/zamówień** — paginacja, rate limiting,
   retry, checkpoint zdarzeń, pełne uzgodnienie i testy na fixture'ach API.
4. **Synchronizacja billingowa** — katalog typów, podpisane wpisy, łączenie po
   zamówieniu/ofercie, deduplikacja i obsługa spóźnionych korekt.
5. **Koszty użytkownika** — formularz, import CSV, wersjonowanie kosztu produktu,
   reguły wysyłki, walidacja i raport błędów.
6. **Silnik rentowności** — formuły, alokacja, kompletność, ochrona przed
   podwójnym liczeniem, testy tabelaryczne i przypadki rabatów/dopłat.
7. **Widoki zamówień i ofert** — filtry okresu, drill-down, ślad źródłowy,
   ostrzeżenia o niepełnych danych i eksport wyniku.
8. **Operacyjność i pilotaż** — harmonogram synchronizacji, obserwowalność bez
   danych wrażliwych, alarmy opóźnień, retencja danych i test z realnymi kontami
   sandbox/produkcyjnymi po uzyskaniu zgód.

Każdy etap powinien dostarczać testy kontraktowe lub tabelaryczne. Przed startem
implementacji trzeba utrwalić przykładowe, zanonimizowane odpowiedzi aktualnej
wersji API dla zwykłego zamówienia, rabatu, dopłaty, korekty prowizji i
zamówienia wielopozycyjnego.
