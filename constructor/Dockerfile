FROM python:3.12-slim
WORKDIR /app
COPY constructor/requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir -r requirements.txt
COPY constructor /app/constructor
ENV CONSTRUCTOR_DATA_DIR=/data PYTHONDONTWRITEBYTECODE=1
CMD ["uvicorn", "constructor.backend.app:app", "--host", "0.0.0.0", "--port", "8077", "--root-path", "/constructor"]
