uvicorn app.main:app --host 0.0.0.0 --port 8000
# rq worker -u redis://${REDIS_HOST} &
#
# wait -n
# exit $?
