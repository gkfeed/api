-- Apply through gkfeed/infra before deploying the v2 item sync API.
-- The API role only needs SELECT; the trigger function owns writes.
CREATE TABLE public.item_changes (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id integer NOT NULL,
    item_id integer NOT NULL,
    payload jsonb,
    changed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX item_changes_user_id_id_idx ON public.item_changes (user_id, id);
GRANT SELECT ON public.item_changes TO gkfeed_api;

CREATE FUNCTION public.record_item_change() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
    old_user integer;
    new_user integer;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        SELECT user_id INTO old_user FROM public.feed WHERE id = OLD.feed_id;
    END IF;
    IF TG_OP <> 'DELETE' THEN
        SELECT user_id INTO new_user FROM public.feed WHERE id = NEW.feed_id;
    END IF;
    -- Serialise sequence allocation and commit for each user. Otherwise a
    -- late commit could appear below a cursor already issued to a client.
    IF old_user IS NOT NULL AND new_user IS NOT NULL AND old_user <> new_user THEN
        PERFORM pg_advisory_xact_lock(193648, LEAST(old_user, new_user));
        PERFORM pg_advisory_xact_lock(193648, GREATEST(old_user, new_user));
    ELSIF COALESCE(old_user, new_user) IS NOT NULL THEN
        PERFORM pg_advisory_xact_lock(193648, COALESCE(old_user, new_user));
    END IF;
    IF old_user IS NOT NULL AND (TG_OP = 'DELETE' OR old_user IS DISTINCT FROM new_user) THEN
        INSERT INTO public.item_changes (user_id, item_id) VALUES (old_user, OLD.id);
    END IF;
    IF new_user IS NOT NULL THEN
        INSERT INTO public.item_changes (user_id, item_id, payload) VALUES
        (new_user, NEW.id, jsonb_build_object(
            'ID', NEW.id, 'FeedID', NEW.feed_id, 'Title', NEW.title,
            'Text', NEW.text, 'Date', NEW.date, 'Link', NEW.link));
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER item_change_after_write
AFTER INSERT OR UPDATE OR DELETE ON public.item
FOR EACH ROW EXECUTE FUNCTION public.record_item_change();

-- Cover feed deletion by database cascades as well as the API's explicit item
-- deletion. When the API already removed items, this trigger has no work.
CREATE FUNCTION public.record_feed_item_deletions() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(193648, OLD.user_id);
    INSERT INTO public.item_changes (user_id, item_id)
    SELECT OLD.user_id, id FROM public.item WHERE feed_id = OLD.id ORDER BY id;
    RETURN OLD;
END;
$$;
CREATE TRIGGER feed_items_before_delete
BEFORE DELETE ON public.feed
FOR EACH ROW EXECUTE FUNCTION public.record_feed_item_deletions();
