@php
    $m = $match['data'] ?? null;
    $err = $match['error'] ?? null;
    $next = $status['session_api_next'] ?? '';
    $text = null;
    if ($err === \MarshmallowMia\EchoVrMatch\Services\MatchReader::MISSING) {
        $text = trans('echovr-match::match.api.missing');
    } elseif ($err) {
        $text = trans('echovr-match::match.api.unreachable');
    } elseif (!$apiOn) {
        $text = trans('echovr-match::match.api.is_off');
    } elseif ($m && ($m['active'] ?? false)) {
        $text = match ($m['api'] ?? '') {
            'disabled' => trans('echovr-match::match.api.disabled'),
            'unavailable' => trans('echovr-match::match.api.unavailable', ['error' => $m['api_error'] ?? '']),
            'waiting' => trans('echovr-match::match.api.waiting'),
            default => null,
        };
    }
    if ($next !== '' && $next !== ($status['session_api'] ?? '')) {
        $text = trim(($text ?? '') . ' ' . trans('echovr-match::match.api.pending', ['state' => trans('echovr-match::match.api.' . $next)]));
    }
@endphp
@if ($text)
    <p style="margin-top:.75rem;font-size:.875rem;opacity:.75">{{ $text }}</p>
@endif
